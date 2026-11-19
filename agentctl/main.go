// Command agentctl controls and installs the launchd-managed agent.
//
// This started life as a 357-line shell script, and the shell was never the point: the work is
// process supervision (launchd), sqlite bookkeeping, log rotation and .env reading. Every quoting
// mistake in bash here was a real bug — `tr -d '"'` once broke the whole status path, and a path
// with a space would silently render a broken plist — so the logic lives in Go now. The command
// surface and every launchd label are unchanged: ./scripts/agentctl is a thin shim over this binary.
package main

import (
	"fmt"
	"os"
)

const usage = `usage: agentctl <command>

  install           render + load the agent job (auto-start, auto-restart)
  install-backup    render + load the daily 04:10 sqlite backup
  install-gateway   build and (re)load the feishu-gw transport job
  uninstall         unload both jobs
  start | stop | restart [--force]   restart waits for the running turn to settle
  restart-later     queue the same wait as its own launchd one-shot job
  restart-once      what that job runs (not for humans)
  status            launchd state + whether the bot can still hear Feishu
  logs [n]          agent log tail
  logs-gateway [n]  feishu-gw log tail
  rotate            trim logs now (LOG_MAX_BYTES, default 5 MB x 3)
  backup            run a backup now
  backups [n]       list snapshots
  restore <file>    restore a snapshot (stops the agent first)
  doctor            diagnose why it will not start
`

func main() {
	args := os.Args[1:]
	command := "status"
	if len(args) > 0 {
		command = args[0]
		args = args[1:]
	}
	if err := dispatch(command, args); err != nil {
		fmt.Fprintf(os.Stderr, "agentctl %s: %v\n", command, err)
		os.Exit(exitCode(err))
	}
}

// exitError carries a specific status without wrapping the message twice.
type exitError struct {
	code int
	err  error
}

func (e *exitError) Error() string { return e.err.Error() }

func exitWith(code int, format string, values ...any) error {
	return &exitError{code: code, err: fmt.Errorf(format, values...)}
}

func exitCode(err error) int {
	if e, ok := err.(*exitError); ok {
		return e.code
	}
	return 1
}

func dispatch(command string, args []string) error {
	p, err := findProject()
	if err != nil {
		return err
	}
	switch command {
	case "install":
		return install(p)
	case "install-gateway":
		return installGateway(p)
	case "install-backup":
		return installBackup(p)
	case "uninstall":
		return uninstall(p)
	case "start":
		return start(p)
	case "stop":
		return stop(p)
	case "restart":
		force := len(args) > 0 && args[0] == "--force"
		return restart(p, force)
	case "restart-later":
		return restartLater(p)
	case "restart-once":
		return restartOnce(p)
	case "status":
		return status(p)
	case "logs":
		return tailFile(p.logPath("agent.log"), argInt(args, 50))
	case "logs-gateway":
		return tailFile(p.logPath("feishu-gw.log"), argInt(args, 50))
	case "rotate":
		p.rotateLogs()
		return nil
	case "backup":
		return backup(p)
	case "backups":
		return listBackups(p, argInt(args, 10))
	case "restore":
		return restore(p, arg(args, 0))
	case "doctor":
		return doctor(p)
	case "help", "-h", "--help":
		fmt.Print(usage)
		return nil
	default:
		fmt.Fprint(os.Stderr, usage)
		return exitWith(2, "unknown command %q", command)
	}
}

func arg(args []string, index int) string {
	if index < len(args) {
		return args[index]
	}
	return ""
}

func argInt(args []string, fallback int) int {
	value := arg(args, 0)
	if value == "" {
		return fallback
	}
	number := 0
	if _, err := fmt.Sscanf(value, "%d", &number); err != nil || number <= 0 {
		return fallback
	}
	return number
}
