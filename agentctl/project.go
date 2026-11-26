package main

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// The launchd labels. These are the contract with everything already installed: the plist files on
// disk, client-go's Restart/Backup buttons, and the notes src/restart.ts reads back.
const (
	labelAgent = "com.pilot.agent"
	labelGW    = labelAgent + ".feishu-gw"
	labelBack  = labelAgent + ".backup"
	// A restart asked for from inside a turn cannot do the bootout itself: it would kill the turn
	// mid-sentence. This one-shot job does the waiting instead, and survives the agent going away.
	labelLater = labelAgent + ".restart-later"
)

const defaultLogMaxBytes = 5 * 1024 * 1024

type project struct {
	root string
	home string
}

func (p project) agentsDir() string { return filepath.Join(p.home, "Library", "LaunchAgents") }

func (p project) plistPath(label string) string { return filepath.Join(p.agentsDir(), label+".plist") }

func (p project) logPath(name string) string { return filepath.Join(p.root, "logs", name) }

func (p project) template(name string) string { return filepath.Join(p.root, "launchd", name) }

func (p project) backupDir() string {
	if dir := os.Getenv("BACKUP_DIR"); dir != "" {
		return dir
	}
	return filepath.Join(p.root, "backups")
}

// dbPath honours AGENT_DB, which .env sets relative to the project root.
func (p project) dbPath() string {
	path := os.Getenv("AGENT_DB")
	if path == "" {
		return filepath.Join(p.root, ".data", "agent.sqlite")
	}
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(p.root, path)
}

// findProject locates the repo the way the shell version did — `dirname $0/..` — except the binary
// lives in bin/, so walking up from the executable is the equivalent. AGENT_HOME overrides it.
func findProject() (project, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return project{}, err
	}
	candidates := []string{os.Getenv("AGENT_HOME")}
	if exe, err := os.Executable(); err == nil {
		if resolved, err := filepath.EvalSymlinks(exe); err == nil {
			exe = resolved
		}
		candidates = append(candidates, filepath.Dir(filepath.Dir(exe)))
	}
	if wd, err := os.Getwd(); err == nil {
		candidates = append(candidates, wd)
	}
	for _, dir := range candidates {
		if dir == "" {
			continue
		}
		marker := filepath.Join(dir, "launchd", "agent.plist.tmpl")
		if _, err := os.Stat(marker); err == nil {
			return project{root: dir, home: home}, nil
		}
	}
	return project{}, errors.New("cannot find the agent project (no launchd/agent.plist.tmpl above the binary or in the cwd)")
}

// envValue reads one key out of a dotenv file: first match wins, everything after the first `=` is
// the value, and surrounding quotes are dropped (bash kept them, which turned a quoted token into
// `Bearer "..."` and an auth failure nobody could see).
func envValue(path, key string) string {
	file, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), "\r")
		if !strings.HasPrefix(line, key+"=") {
			continue
		}
		value := line[len(key)+1:]
		if len(value) >= 2 {
			if (value[0] == '"' && value[len(value)-1] == '"') || (value[0] == '\'' && value[len(value)-1] == '\'') {
				value = value[1 : len(value)-1]
			}
		}
		return value
	}
	return ""
}

func envValueOr(path, key, fallback string) string {
	if value := envValue(path, key); value != "" {
		return value
	}
	return fallback
}

// run runs a command attached to our stdio, the way `set -e` did with visible output.
func run(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		if _, ok := err.(*exec.ExitError); ok {
			return exitWith(1, "%s exited non-zero", name)
		}
		return err
	}
	return nil
}

// output runs a command and returns its trimmed stdout, or "" when it failed.
func output(name string, args ...string) string {
	out, err := exec.Command(name, args...).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// command is output with the error kept, for callers that tell "not loaded" from "no output".
func command(name string, args ...string) (string, error) {
	out, err := exec.Command(name, args...).Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func lookPath(name string) (string, error) { return exec.LookPath(name) }

func hasCommand(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func pidAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

// processesMatching is `pgrep -f`: the shell version leaned on it for "is the agent running".
func processesMatching(pattern string) []string {
	out, err := exec.Command("pgrep", "-f", pattern).Output()
	if err != nil {
		return nil
	}
	var pids []string
	for _, field := range strings.Fields(string(out)) {
		pids = append(pids, field)
	}
	return pids
}

// rotateLogs renames generations only below a size floor: launchd appends to these files and never
// reopens them, so a rename is only safe while the job is stopped, and only worth doing when big.
func (p project) rotateLogs() {
	limit := defaultLogMaxBytes
	if raw := os.Getenv("LOG_MAX_BYTES"); raw != "" {
		if value, err := strconv.Atoi(raw); err == nil && value > 0 {
			limit = value
		}
	}
	for _, base := range []string{"agent.log", "agent.err.log", "backup.log", "backup.err.log"} {
		path := p.logPath(base)
		info, err := os.Stat(path)
		if err != nil || info.Size() < int64(limit) {
			continue
		}
		size := info.Size()
		os.Remove(path + ".3")
		for generation := 2; generation >= 1; generation-- {
			from := fmt.Sprintf("%s.%d", path, generation)
			to := fmt.Sprintf("%s.%d", path, generation+1)
			if _, err := os.Stat(from); err == nil {
				os.Rename(from, to)
			}
		}
		os.Rename(path, path+".1")
		if file, err := os.Create(path); err == nil {
			file.Close()
		}
		fmt.Printf("rotated %s (%d KB → %s.1)\n", base, size/1024, base)
	}
}

func tailFile(path string, lines int) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	rows := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	if len(rows) > lines {
		rows = rows[len(rows)-lines:]
	}
	fmt.Println(strings.Join(rows, "\n"))
	return nil
}

// humanBytes mirrors `du -h` closely enough for a status line.
func humanBytes(size int64) string {
	units := []string{"K", "M", "G", "T"}
	value := float64(size) / 1024
	unit := 0
	for value >= 1024 && unit < len(units)-1 {
		value /= 1024
		unit++
	}
	if unit == 0 && value < 1 {
		return fmt.Sprintf("%.1fK", value)
	}
	if value >= 10 {
		return fmt.Sprintf("%.0f%s", value, units[unit])
	}
	return fmt.Sprintf("%.1f%s", value, units[unit])
}

// duSize reports allocated blocks the way `du -h` does: a 201 MB database occupies 213 MB on disk,
// and doctor was quoting the first number for years.
func duSize(path string, info os.FileInfo) string {
	if stat, ok := info.Sys().(*syscall.Stat_t); ok && stat.Blocks > 0 {
		return humanBytes(stat.Blocks * 512)
	}
	return humanBytes(info.Size())
}

// humanFree is `df -h`: binary units spelled with an i, because that is what the reader compares
// against Finder.
func humanFree(bytes int64) string {
	value := float64(bytes) / (1 << 10)
	for _, unit := range []string{"Ki", "Mi", "Gi", "Ti"} {
		if value < 1024 {
			return fmt.Sprintf("%.0f%s", value, unit)
		}
		value /= 1024
	}
	return fmt.Sprintf("%.0fPi", value)
}
