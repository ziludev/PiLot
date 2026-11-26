package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"pilot/toolbox/internal/proto"
	"pilot/toolbox/internal/server"
)

// BashArgs mirrors the `bash` tool's parameters: pi-schema validated them on the
// agent side, so unknown fields are ignored rather than rejected.
type BashArgs struct {
	Command    string            `json:"command"`
	Timeout    *float64          `json:"timeout,omitempty"`
	Env        map[string]string `json:"env,omitempty"`
	InheritEnv *bool             `json:"inheritEnv,omitempty"`
}

// BashResult is what the agent turns into the tool's structured output. Output
// text and truncation stay on the agent side: it is the harness that keeps the
// tail, so only the facts only the process can know come back from here.
type BashResult struct {
	ExitCode       int    `json:"exitCode"`
	FullOutputPath string `json:"fullOutputPath,omitempty"`
	// TimedOut is for the call log only; the agent sees a timeout as a failure.
	TimedOut bool `json:"-"`
}

// maxTimeoutSeconds matches the agent's guard, so both paths reject the same values.
const maxTimeoutSeconds = 2_147_483_647.0 / 1000

// RegisterBash adds the `bash` tool: the command runs here, in its own process
// group, and its output streams to the agent as it arrives.
func RegisterBash(srv *server.Server) {
	srv.Register("bash", func(ctx context.Context, req proto.Request, emit server.Emit) (any, error) {
		var args BashArgs
		if len(req.Args) > 0 {
			if err := json.Unmarshal(req.Args, &args); err != nil {
				return nil, fmt.Errorf("bash: %w", err)
			}
		}
		timeout, err := parseTimeout(args.Timeout)
		if err != nil {
			return nil, err
		}
		// One line per call, so a slow or failing command is visible in the log.
		started := time.Now()
		result, runErr := runBash(ctx, req.Cwd, args, timeout, emit)
		took := time.Since(started).Round(time.Millisecond)
		var failure *server.Failure
		if errors.As(runErr, &failure) {
			srv.Logf("bash: tool=bash failed=%s timeout=%t took=%s", failure.Code, failure.Code == "timeout", took)
		} else {
			// timeout= is on every line, so a slow call and a timed-out one look the same in the log.
			srv.Logf("bash: tool=bash exit=%d spill=%t timeout=%t took=%s", result.ExitCode, result.FullOutputPath != "", result.TimedOut, took)
		}
		return result, runErr
	})
}

func parseTimeout(seconds *float64) (time.Duration, error) {
	if seconds == nil {
		return 0, nil
	}
	value := *seconds
	if math.IsNaN(value) || math.IsInf(value, 0) || value <= 0 {
		return 0, errors.New("Invalid timeout: must be a finite number of seconds")
	}
	if value > maxTimeoutSeconds {
		return 0, fmt.Errorf("Invalid timeout: maximum is %v seconds", maxTimeoutSeconds)
	}
	return time.Duration(value * float64(time.Second)), nil
}

func runBash(ctx context.Context, dir string, args BashArgs, timeout time.Duration, emit server.Emit) (BashResult, error) {
	sh, ok := findShell("")
	if !ok {
		return BashResult{}, errors.New("No bash shell found")
	}
	cwd := dir
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	// The agent's environment reports a missing cwd as a spawn error with this exact
	// text, and the tool's message is model-visible.
	if info, err := os.Stat(cwd); err != nil || !info.IsDir() {
		return BashResult{}, fmt.Errorf("Working directory does not exist: %s\nCannot execute bash commands.", cwd)
	}

	cmd := exec.Command(sh.program, append(append([]string(nil), sh.args...), args.Command)...)
	cmd.Dir = cwd
	cmd.Env = environ(args)
	// Its own process group, so a timeout or an abort kills what the command
	// started, not just the shell.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return BashResult{}, &server.Failure{Code: "spawn_error", Message: err.Error()}
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return BashResult{}, &server.Failure{Code: "spawn_error", Message: err.Error()}
	}
	if err := cmd.Start(); err != nil {
		return BashResult{}, &server.Failure{Code: "spawn_error", Message: err.Error()}
	}

	var timedOut atomic.Bool
	finished := make(chan struct{})
	defer close(finished)
	kill := func() {
		if cmd.Process == nil {
			return
		}
		// Negative pid: the whole group. A command that ignores it still holds the
		// pipes, so the reads below end only when the group is really gone.
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	if timeout > 0 {
		go func() {
			select {
			case <-time.After(timeout):
				timedOut.Store(true)
				kill()
			case <-finished:
			}
		}()
	}
	go func() {
		select {
		case <-ctx.Done():
			kill()
		case <-finished:
		}
	}()

	saved := &spill{}
	var emitMu sync.Mutex
	stream := func(pipe interface{ Read([]byte) (int, error) }) {
		decoder := &utf8Stream{}
		buf := make([]byte, 32*1024)
		for {
			n, readErr := pipe.Read(buf)
			if n > 0 {
				chunk := buf[:n]
				saved.add(chunk)
				text := decoder.write(chunk)
				if text != "" {
					emitMu.Lock()
					emit(proto.Event{Type: proto.TypeChunk, Data: text})
					emitMu.Unlock()
				}
			}
			if readErr != nil {
				break
			}
		}
		if text := decoder.flush(); text != "" {
			emitMu.Lock()
			emit(proto.Event{Type: proto.TypeChunk, Data: text})
			emitMu.Unlock()
		}
	}
	var readers sync.WaitGroup
	readers.Add(2)
	go func() { defer readers.Done(); stream(stdout) }()
	go func() { defer readers.Done(); stream(stderr) }()
	readers.Wait()

	waitErr := cmd.Wait()
	saved.close()
	exitCode := exitCodeOf(cmd, waitErr)
	path := saved.Path()

	// A timeout and an abort are failures whatever the exit code says, and both
	// still report the spilled output: the agent turns it into a diagnostic before
	// it throws.
	switch {
	case timedOut.Load():
		return BashResult{}, &server.Failure{
			Code:      "timeout",
			Message:   fmt.Sprintf("timeout:%v", timeout.Seconds()),
			SpillPath: path,
		}
	case ctx.Err() != nil:
		return BashResult{}, &server.Failure{Code: "aborted", Message: "aborted", SpillPath: path}
	case waitErr != nil:
		if _, isExit := waitErr.(*exec.ExitError); !isExit {
			return BashResult{}, &server.Failure{Code: "spawn_error", Message: waitErr.Error(), SpillPath: path}
		}
	}
	return BashResult{ExitCode: exitCode, FullOutputPath: path, TimedOut: timedOut.Load()}, nil
}

// exitCodeOf maps a signal death to 128+signal, as the agent's environment does,
// so a killed command is never mistaken for a successful one.
func exitCodeOf(cmd *exec.Cmd, waitErr error) int {
	if waitErr == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(waitErr, &exitErr) {
		if status, ok := exitErr.Sys().(syscall.WaitStatus); ok && status.Signaled() {
			return 128 + int(status.Signal())
		}
		return exitErr.ExitCode()
	}
	return 1
}

// environ builds the child's environment: the agent's, then the call's extra
// values, unless the fence asked for a bare environment.
func environ(args BashArgs) []string {
	inherit := args.InheritEnv == nil || *args.InheritEnv
	env := []string{}
	if inherit {
		env = os.Environ()
	}
	for key, value := range args.Env {
		env = append(env, key+"="+value)
	}
	return env
}
