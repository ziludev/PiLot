package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

func (p project) keepSnapshots() int {
	if raw := os.Getenv("KEEP"); raw != "" {
		if value, err := strconv.Atoi(raw); err == nil && value > 0 {
			return value
		}
	}
	return 14
}

// backup takes a hot snapshot through SQLite's own `.backup`, safe while the agent runs: the WAL is
// folded in instead of copied file by file, and the result is checked before it is trusted.
func backup(p project) error {
	db := p.dbPath()
	if _, err := os.Stat(db); err != nil {
		fmt.Printf("no database at %s, nothing to back up\n", db)
		return nil
	}
	if !hasCommand("sqlite3") {
		return fmt.Errorf("sqlite3 not found in PATH")
	}
	destination := p.backupDir()
	if err := os.MkdirAll(destination, 0o755); err != nil {
		return err
	}
	out := filepath.Join(destination, "agent-"+nowFn().Format("20060102-150405")+".sqlite")
	if err := sqliteCommand(db, ".backup '"+strings.ReplaceAll(out, "'", "''")+"'"); err != nil {
		return err
	}
	// The snapshot inherits WAL mode; consolidate so one file is a complete backup.
	_ = sqliteCommand(out, "PRAGMA journal_mode=DELETE;")
	os.Remove(out + "-wal")
	os.Remove(out + "-shm")
	if check := sqliteOutput(out, "PRAGMA integrity_check;"); check != "ok" {
		os.Remove(out)
		return fmt.Errorf("backup FAILED integrity check: %s", out)
	}
	info, err := os.Stat(out)
	if err != nil {
		return err
	}
	fmt.Printf("%s backup ok: %s (%s)\n", timestamp(), filepath.Base(out), humanBytes(info.Size()))

	found := snapshots(destination)
	if keep := p.keepSnapshots(); len(found) > keep {
		for _, old := range found[keep:] {
			os.Remove(old)
			fmt.Printf("%s pruned: %s\n", timestamp(), filepath.Base(old))
		}
	}
	return nil
}

func sqliteCommand(path, statement string) error {
	cmd := exec.Command("sqlite3", path, statement)
	cmd.Stdout = io.Discard
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("sqlite3 %s: %v: %s", filepath.Base(path), err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

func sqliteOutput(path, statement string) string {
	out, err := exec.Command("sqlite3", path, statement).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// snapshots lists agent-*.sqlite newest first: the order both the listing and the pruning rely on.
func snapshots(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	type snapshot struct {
		path    string
		modTime int64
	}
	var found []snapshot
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasPrefix(name, "agent-") || !strings.HasSuffix(name, ".sqlite") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		found = append(found, snapshot{path: filepath.Join(dir, name), modTime: info.ModTime().UnixNano()})
	}
	sort.Slice(found, func(a, b int) bool { return found[a].modTime > found[b].modTime })
	paths := make([]string, 0, len(found))
	for _, item := range found {
		paths = append(paths, item.path)
	}
	return paths
}

func listBackups(p project, limit int) error {
	found := snapshots(p.backupDir())
	if len(found) == 0 {
		fmt.Println("no backups yet")
		return nil
	}
	if limit > len(found) {
		limit = len(found)
	}
	for _, path := range found[:limit] {
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		fmt.Printf("%8s  %s  %s\n", humanBytes(info.Size()), info.ModTime().Format("Jan _2 15:04"), filepath.Base(path))
	}
	return nil
}

// restore replaces the database, and only after the process that owns it is really gone: swapping the
// file under a live connection leaves a stale WAL behind and loses the restore.
func restore(p project, source string) error {
	if source == "" {
		return exitWith(2, "usage: agentctl restore <backup file>")
	}
	if info, err := os.Stat(source); err != nil || info.IsDir() {
		return exitWith(2, "no such backup: %s", source)
	}
	fmt.Println("stopping agent before replacing the database")
	_ = launchctl("kill", "SIGTERM", domain()+"/"+labelAgent)
	pattern := filepath.Join(p.root, "src", "main.ts")
	for attempt := 0; attempt < 20; attempt++ {
		if len(processesMatching(pattern)) == 0 {
			break
		}
		sleepSeconds(1)
	}
	if len(processesMatching(pattern)) > 0 {
		return fmt.Errorf("agent still running after 20s; aborting restore")
	}
	db := p.dbPath()
	if _, err := os.Stat(db); err == nil {
		_ = copyFile(db, db+".before-restore-"+nowFn().Format("20060102-150405"))
	}
	if err := copyFile(source, db); err != nil {
		return err
	}
	os.Remove(db + "-wal")
	os.Remove(db + "-shm")
	fmt.Printf("restored %s → %s; starting agent\n", source, db)
	if err := launchctl("kickstart", domain()+"/"+labelAgent); err != nil {
		return fmt.Errorf("launchctl kickstart %s: %w", labelAgent, err)
	}
	return nil
}

func copyFile(from, to string) error {
	source, err := os.Open(from)
	if err != nil {
		return err
	}
	defer source.Close()
	if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
		return err
	}
	destination, err := os.Create(to)
	if err != nil {
		return err
	}
	if _, err := io.Copy(destination, source); err != nil {
		destination.Close()
		return err
	}
	return destination.Close()
}
