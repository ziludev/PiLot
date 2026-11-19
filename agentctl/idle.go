package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Indirection so tests can drive the clock instead of waiting two real seconds per poll.
var (
	sleepFn = time.Sleep
	nowFn   = time.Now
)

func sleepSeconds(seconds int) { sleepFn(time.Duration(seconds) * time.Second) }

func timestamp() string { return nowFn().Format("2006-01-02 15:04:05") }

func (p project) sqlite(query string) string {
	path := p.dbPath()
	if _, err := os.Stat(path); err != nil || !hasCommand("sqlite3") {
		return ""
	}
	// Read-only URI: a control command must never be the second writer.
	out, err := exec.Command("sqlite3", "file:"+path+"?mode=ro", query).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// turnsInFlight: a queued or placed submission is a turn that has not settled yet — model call,
// tool call, delivery. Restarting under it cuts the turn, and the surface then has to recover a
// half-written card, which is how a "quick restart" turns into a duplicated answer.
func (p project) turnsInFlight() int {
	count, err := strconv.Atoi(p.sqlite("select count(*) from submissions where status in ('queued','placed')"))
	if err != nil {
		return 0
	}
	return count
}

// busyChats says which conversation is holding the restart up: a count alone says the agent is busy,
// not that one chat has been mid-turn for half an hour.
func (p project) busyChats() string {
	return p.sqlite("select group_concat(conversation_id, ',') from submissions where status in ('queued','placed')")
}

// waitIdle blocks until nothing is in flight or the window closes. A single chat's turn can run for
// twenty minutes and the queue is shared by every chat, so the window is ten minutes, not three:
// 180s lost the race five times in a row on 11-19 while another chat worked.
func (p project) waitIdle() bool {
	timeout := 600
	if raw := os.Getenv("RESTART_IDLE_TIMEOUT"); raw != "" {
		if value, err := strconv.Atoi(raw); err == nil && value > 0 {
			timeout = value
		}
	}
	count := p.turnsInFlight()
	if count == 0 {
		return true
	}
	fmt.Printf("agent has %d turn(s) in flight (%s); waiting up to %ds for them to settle\n", count, p.busyChats(), timeout)
	deadline := nowFn().Add(time.Duration(timeout) * time.Second)
	for {
		if p.turnsInFlight() == 0 {
			fmt.Println("agent idle; restarting")
			return true
		}
		if !nowFn().Before(deadline) {
			fmt.Fprintf(os.Stderr, "still mid-turn after %ds — not restarting.\n", timeout)
			fmt.Fprintln(os.Stderr, "the turn is running in this very process if you started agentctl from inside it;")
			fmt.Fprintln(os.Stderr, "use 'agentctl restart --force' to cut it (the turn resumes on the next start, but any")
			fmt.Fprintln(os.Stderr, "half-delivered card is recovered by the surface, not resent).")
			return false
		}
		sleepSeconds(2)
	}
}

// restartNote is the file src/restart.ts reads on boot, so the agent can say why it went away. The
// shape is a contract with that reader: kind, reason, queuedAt, at.
type restartNote struct {
	Kind     string `json:"kind"`
	Reason   string `json:"reason"`
	QueuedAt int64  `json:"queuedAt"`
	At       int64  `json:"at"`
}

func (p project) notePath() string {
	return filepath.Join(filepath.Dir(p.dbPath()), "restart-pending.json")
}

// noteRestart keeps the original queue time and reason across a queued → planned handoff: a restart
// can wait twenty minutes for an idle window, and the wait is what makes the card honest.
func (p project) noteRestart(kind, reason string) error {
	path := p.notePath()
	now := nowFn().UnixMilli()
	previous := restartNote{}
	if data, err := os.ReadFile(path); err == nil {
		// A half-written note is not worth failing the switch over; it just starts fresh.
		_ = json.Unmarshal(data, &previous)
	}
	note := restartNote{
		Kind:     kind,
		Reason:   reason,
		QueuedAt: previous.QueuedAt,
		At:       now,
	}
	if note.Reason == "" {
		note.Reason = previous.Reason
	}
	if note.QueuedAt == 0 {
		note.QueuedAt = now
	}
	data, err := json.Marshal(note)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}
