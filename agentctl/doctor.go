package main

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	green  = "\033[32m"
	yellow = "\033[33m"
	red    = "\033[31m"
	reset  = "\033[0m"
)

func okLine(format string, values ...any) {
	fmt.Printf("  %s✓%s %s\n", green, reset, fmt.Sprintf(format, values...))
}
func warnLine(format string, values ...any) {
	fmt.Printf("  %s!%s %s\n", yellow, reset, fmt.Sprintf(format, values...))
}
func badLine(format string, values ...any) {
	fmt.Printf("  %s✗%s %s\n", red, reset, fmt.Sprintf(format, values...))
}

var (
	reportLine  = regexp.MustCompile(`^\[report\] \d{4}-\d{2}-\d{2}T`)
	legacyLine  = regexp.MustCompile(`^\[report\] (storage_failed|task_faulted|hook_failed|tool_failed)`)
	digitsOnly  = regexp.MustCompile(`[^0-9]`)
	doctorState = "doctor.restarts"
)

// doctor walks the usual reasons the agent will not start or keeps restarting. It judges relatively:
// restarts as a delta since the previous run, errors from the last two days only, and untimestamped
// old log lines counted separately so they cannot keep posing as a new fault.
func doctor(p project) error {
	env := filepath.Join(p.root, ".env")
	db := p.dbPath()

	fmt.Println("node")
	if path, err := lookPath("node"); err == nil {
		okLine("%s at %s", output("node", "-v"), path)
	} else {
		badLine("node not found in PATH")
	}
	if hasCommand("git") {
		okLine("%s", output("git", "--version"))
	} else {
		badLine("git not found (worktrees and diffs need it)")
	}

	fmt.Println("config")
	if _, err := os.Stat(env); err == nil {
		okLine(".env present")
	} else {
		warnLine(".env missing (copy .env.example)")
	}
	for _, key := range []string{"FEISHU_APP_ID", "FEISHU_APP_SECRET"} {
		value := envValue(env, key)
		if value != "" {
			okLine("%s set (%s…)", key, clip(value, 8))
		} else {
			badLine("%s missing in .env", key)
		}
	}
	switch model := envValue(env, "AGENT_MODEL"); {
	case strings.HasPrefix(model, "auto/") || strings.HasPrefix(model, "group/"):
		warnLine("AGENT_MODEL=%s is a routing group (results vary run to run)", model)
	case model == "":
		warnLine("AGENT_MODEL unset")
	default:
		okLine("AGENT_MODEL=%s (pinned)", model)
	}

	fmt.Println("storage")
	if info, err := os.Stat(db); err == nil {
		if sqliteOutput(db, "PRAGMA quick_check;") == "ok" {
			okLine("%s (%s) quick_check ok", db, duSize(db, info))
		} else {
			badLine("%s failed quick_check", db)
		}
	} else {
		warnLine("%s does not exist yet (created on first start)", db)
	}
	if data, err := os.ReadFile(filepath.Join(filepath.Dir(db), "agent.owner")); err == nil {
		holder, _ := strconv.Atoi(strings.TrimSpace(string(data)))
		if pidAlive(holder) {
			warnLine("storage held by pid %d (an agent is running)", holder)
		} else {
			badLine("stale lock: .data/agent.owner names dead pid %d", holder)
		}
	} else {
		okLine("no storage lock")
	}

	fmt.Println("process")
	pids := processesMatching(filepath.Join(p.root, "src", "main.ts"))
	switch {
	case len(pids) == 0:
		warnLine("no agent process running")
	case len(pids) > 1:
		badLine("multiple agent processes (%s) — one storage, one writer", strings.Join(pids, " "))
	default:
		okLine("agent pid %s", pids[0])
	}
	port := envValue(env, "AGENT_HTTP_PORT")
	if port == "" {
		port = "8787"
	}
	if answers("http://127.0.0.1:" + port + "/api/state") {
		okLine("client bridge answers on :%s", port)
	} else {
		badLine("client bridge not answering on :%s", port)
	}

	fmt.Println("launchd")
	printed, printErr := launchctlOutput("print", domain()+"/"+labelAgent)
	if printErr == nil && strings.Contains(printed, "state = running") {
		okLine("%s running", labelAgent)
	} else {
		warnLine("%s not running (./scripts/agentctl install)", labelAgent)
	}
	if runs := runsCount(printed); runs != "" {
		statePath := filepath.Join(p.root, ".data", doctorState)
		baseline := ""
		if data, err := os.ReadFile(statePath); err == nil {
			baseline = digitsOnly.ReplaceAllString(string(data), "")
		}
		total, _ := strconv.Atoi(runs)
		if baseline == "" {
			okLine("%s restarts total (baseline recorded)", runs)
		} else {
			previous, _ := strconv.Atoi(baseline)
			if delta := total - previous; delta >= 3 {
				warnLine("restarted %d times since the last doctor run (total %s): check logs/agent.err.log", delta, runs)
			} else {
				okLine("%s restarts total, +%d since last check", runs, delta)
			}
		}
		_ = os.MkdirAll(filepath.Dir(statePath), 0o755)
		_ = os.WriteFile(statePath, []byte(runs), 0o644)
	}

	fmt.Println("workspace")
	workspace := filepath.Join(p.root, "workspace")
	if info, err := os.Stat(filepath.Join(workspace, ".git")); err == nil && info.IsDir() {
		okLine("workspace is a git repo (%s)", output("git", "-C", workspace, "rev-parse", "--abbrev-ref", "HEAD"))
		okLine("%d worktrees (one per conversation)", nonEmptyLines(output("git", "-C", workspace, "worktree", "list")))
	} else {
		warnLine("workspace is not a git repo yet (created at first start)")
	}
	if free, err := diskFree(p.root); err == nil {
		fmt.Printf("  · disk free: %s\n", humanFree(free))
	}

	fmt.Println("recent errors")
	if errLog := p.logPath("agent.err.log"); fileExists(errLog) {
		today := time.Now().UTC().Format("2006-01-02")
		yesterday := time.Now().UTC().AddDate(0, 0, -1).Format("2006-01-02")
		var recent []string
		legacy := 0
		for _, line := range strings.Split(readText(errLog), "\n") {
			if legacyLine.MatchString(line) {
				legacy++
			}
			if reportLine.MatchString(line) && (strings.Contains(line, today) || strings.Contains(line, yesterday)) {
				recent = append(recent, line)
			}
		}
		if len(recent) > 5 {
			recent = recent[len(recent)-5:]
		}
		if len(recent) == 0 {
			okLine("nothing in the last 2 days")
		} else {
			for _, line := range recent {
				fmt.Printf("  · %s\n", line)
			}
		}
		if legacy > 0 {
			fmt.Printf("  · %d older report line(s) without a timestamp (before this version; check logs/agent.err.log)\n", legacy)
		}
	} else {
		warnLine("no logs/agent.err.log yet")
	}
	return nil
}

// runsCount reads launchd's cumulative restart counter. It counts every deliberate agentctl restart
// too, so the number alone means nothing — the caller compares it with the previous run.
func runsCount(printed string) string {
	for _, line := range strings.Split(printed, "\n") {
		if !strings.Contains(line, "runs = ") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		return fields[len(fields)-1]
	}
	return ""
}

func answers(url string) bool {
	client := &http.Client{Timeout: 2 * time.Second}
	response, err := client.Get(url)
	if err != nil {
		return false
	}
	response.Body.Close()
	return true
}

func diskFree(path string) (int64, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0, err
	}
	return int64(stat.Bavail) * int64(stat.Bsize), nil
}

func readText(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func nonEmptyLines(text string) int {
	count := 0
	for _, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) != "" {
			count++
		}
	}
	return count
}

func clip(text string, max int) string {
	runes := []rune(text)
	if len(runes) <= max {
		return text
	}
	return string(runes[:max])
}
