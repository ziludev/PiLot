package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeClock pins now() and makes sleeping free, so the idle window can be tested without waiting.
func fakeClock(t *testing.T, start time.Time, step time.Duration) {
	t.Helper()
	previousNow, previousSleep := nowFn, sleepFn
	current := start.Add(-step)
	nowFn = func() time.Time {
		current = current.Add(step)
		return current
	}
	sleepFn = func(time.Duration) {}
	t.Cleanup(func() { nowFn, sleepFn = previousNow, previousSleep })
}

// fakeSQLite puts a sqlite3 on PATH that always answers with `count`, because the pending-turn count
// is the one thing a restart decision hangs on.
func fakeSQLite(t *testing.T, count string) {
	t.Helper()
	dir := t.TempDir()
	script := "#!/bin/sh\necho " + count + "\n"
	if err := os.WriteFile(filepath.Join(dir, "sqlite3"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
}

func tempProject(t *testing.T, dbPath string) project {
	t.Helper()
	root := t.TempDir()
	t.Setenv("AGENT_DB", dbPath)
	return project{root: root, home: t.TempDir()}
}

func TestNoteRestartKeepsQueueTimeAndReason(t *testing.T) {
	base := time.Date(2026, 11, 19, 9, 0, 0, 0, time.UTC)
	fakeClock(t, base, time.Second)
	p := tempProject(t, filepath.Join(t.TempDir(), "agent.sqlite"))

	if err := p.noteRestart("queued", "我排队重启"); err != nil {
		t.Fatal(err)
	}
	first := readNote(t, p)
	if first.Kind != "queued" || first.Reason != "我排队重启" || first.QueuedAt == 0 || first.At != first.QueuedAt {
		t.Fatalf("queued note wrong: %+v", first)
	}

	if err := p.noteRestart("manual", "手动重启"); err != nil {
		t.Fatal(err)
	}
	second := readNote(t, p)
	if second.Kind != "manual" || second.Reason != "手动重启" {
		t.Fatalf("manual note did not replace kind/reason: %+v", second)
	}
	if second.QueuedAt != first.QueuedAt {
		t.Fatalf("queuedAt should survive the handoff: %d → %d", first.QueuedAt, second.QueuedAt)
	}
	if second.At <= first.At {
		t.Fatalf("at should be the fire time, not the queue time: %+v", second)
	}

	// The fire keeps the reason it was queued with: that is what makes the wait visible.
	if err := p.noteRestart("planned", ""); err != nil {
		t.Fatal(err)
	}
	third := readNote(t, p)
	if third.Kind != "planned" || third.Reason != "手动重启" || third.QueuedAt != first.QueuedAt {
		t.Fatalf("planned note lost the queued story: %+v", third)
	}
}

func readNote(t *testing.T, p project) restartNote {
	t.Helper()
	data, err := os.ReadFile(p.notePath())
	if err != nil {
		t.Fatal(err)
	}
	var note restartNote
	if err := json.Unmarshal(data, &note); err != nil {
		t.Fatalf("note is not JSON: %v (%s)", err, data)
	}
	return note
}

func TestWaitIdleReturnsAtOnceWithNoDatabase(t *testing.T) {
	p := tempProject(t, filepath.Join(t.TempDir(), "missing.sqlite"))
	started := time.Now()
	if !p.waitIdle() {
		t.Fatal("a missing database means nothing is in flight")
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("waitIdle blocked for %s with nothing in flight", elapsed)
	}
}

func TestWaitIdleGivesUpWhenATurnNeverSettles(t *testing.T) {
	db := filepath.Join(t.TempDir(), "agent.sqlite")
	if err := os.WriteFile(db, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	fakeSQLite(t, "1")
	fakeClock(t, time.Now(), time.Second)
	t.Setenv("RESTART_IDLE_TIMEOUT", "30")
	p := tempProject(t, db)

	if p.waitIdle() {
		t.Fatal("a turn that never settles must not be treated as idle")
	}
}

func TestWaitIdleSucceedsWhenTheTurnSettles(t *testing.T) {
	db := filepath.Join(t.TempDir(), "agent.sqlite")
	if err := os.WriteFile(db, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	// Busy on the first answer, idle afterwards: the window that a single chat opening and closing.
	answers := "1\n"
	counter := filepath.Join(dir, "count")
	if err := os.WriteFile(counter, []byte(answers), 0o644); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\n" +
		"head -1 " + counter + "\n" +
		"tail -n +2 " + counter + " > " + counter + ".next\n" +
		"mv " + counter + ".next " + counter + "\n"
	if err := os.WriteFile(filepath.Join(dir, "sqlite3"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	fakeClock(t, time.Now(), time.Second)
	p := tempProject(t, db)

	if !p.waitIdle() {
		t.Fatal("waitIdle should return once the queue is empty")
	}
}

func TestRotateLogsShiftsGenerations(t *testing.T) {
	p := tempProject(t, filepath.Join(t.TempDir(), "agent.sqlite"))
	logs := filepath.Join(p.root, "logs")
	if err := os.MkdirAll(logs, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name, content string) {
		if err := os.WriteFile(filepath.Join(logs, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("agent.log", strings.Repeat("x", 2048))
	write("agent.log.1", "one")
	write("agent.log.2", "two")
	write("agent.log.3", "three")
	t.Setenv("LOG_MAX_BYTES", "1024")

	p.rotateLogs()

	expect := map[string]string{
		"agent.log":   "",
		"agent.log.1": strings.Repeat("x", 2048),
		"agent.log.2": "one",
		"agent.log.3": "two",
	}
	for name, want := range expect {
		data, err := os.ReadFile(filepath.Join(logs, name))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if string(data) != want {
			t.Fatalf("%s = %q, want %q", name, string(data), want)
		}
	}
}

func TestRotateLeavesSmallLogsAlone(t *testing.T) {
	p := tempProject(t, filepath.Join(t.TempDir(), "agent.sqlite"))
	logs := filepath.Join(p.root, "logs")
	os.MkdirAll(logs, 0o755)
	path := filepath.Join(logs, "agent.log")
	os.WriteFile(path, []byte("small"), 0o644)
	t.Setenv("LOG_MAX_BYTES", "1024")

	p.rotateLogs()

	if data, _ := os.ReadFile(path); string(data) != "small" {
		t.Fatal("a log below the floor must not be rotated")
	}
	if _, err := os.Stat(path + ".1"); err == nil {
		t.Fatal("no generation should exist for a small log")
	}
}

func TestEnvValueTakesTheFirstMatchAndDropsQuotes(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	body := "FEISHU_APP_ID=cli_abc\nAGENT_HTTP_TOKEN=\"tok=en\"\nAGENT_HTTP_TOKEN=second\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := envValue(path, "AGENT_HTTP_TOKEN"); got != "tok=en" {
		t.Fatalf("value = %q, want the first match with the quotes stripped", got)
	}
	if got := envValueOr(path, "MISSING", "8787"); got != "8787" {
		t.Fatalf("fallback = %q", got)
	}
	if got := envValue(filepath.Join(t.TempDir(), "nope"), "ANY"); got != "" {
		t.Fatalf("missing file should read as empty, got %q", got)
	}
}

func TestSnapshotsNewestFirst(t *testing.T) {
	dir := t.TempDir()
	names := []string{"agent-20260101-041005.sqlite", "agent-20260102-041005.sqlite", "agent-20260103-041005.sqlite"}
	for index, name := range names {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		stamp := time.Now().Add(time.Duration(index) * time.Minute)
		if err := os.Chtimes(path, stamp, stamp); err != nil {
			t.Fatal(err)
		}
	}
	os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("x"), 0o644)

	found := snapshots(dir)
	if len(found) != 3 {
		t.Fatalf("found %d snapshots, want 3 (only agent-*.sqlite)", len(found))
	}
	if filepath.Base(found[0]) != names[2] || filepath.Base(found[2]) != names[0] {
		t.Fatalf("snapshots not newest-first: %v", found)
	}
}

func TestRunsCountReadsLaunchdPrint(t *testing.T) {
	printed := "\tstate = running\n\tprogram = /opt/homebrew/bin/node\n\truns = 41\n\tlast exit code = 0\n"
	if got := runsCount(printed); got != "41" {
		t.Fatalf("runsCount = %q, want 41", got)
	}
	if got := runsCount("\tstate = not running\n"); got != "" {
		t.Fatalf("no runs line should be empty, got %q", got)
	}
}
