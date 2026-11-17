// Command toolbox executes the agent's tools outside the Node process.
//
// The agent spawns it, supervises it, and talks to it over a unix socket in
// `.data/`. Nothing else is shared: no database, no files of the agent's own.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"pitta/toolbox/internal/server"
	"pitta/toolbox/internal/tools"
)

// version is bumped when the wire format changes in a way the agent must notice.
const version = "0.1.0"

func main() {
	socketPath := flag.String("socket", "", "unix socket to listen on")
	showVersion := flag.Bool("version", false, "print the version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println(version)
		return
	}
	if *socketPath == "" {
		fmt.Fprintln(os.Stderr, "toolbox: -socket is required")
		os.Exit(2)
	}

	// Progress goes to stdout and failures to stderr, so the agent can log the two
	// apart: a service that merely says it is listening is not an error.
	info := log.New(os.Stdout, "", log.LstdFlags)
	failures := log.New(os.Stderr, "", log.LstdFlags)
	if err := run(*socketPath, info); err != nil {
		failures.Printf("toolbox: %v", err)
		os.Exit(1)
	}
}

func run(socketPath string, logger *log.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// A socket left behind by a killed agent would make Listen fail and leave the
	// restart hanging, so the path is cleared before binding.
	if err := os.Remove(socketPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(socketPath), 0o700); err != nil {
		return err
	}
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return err
	}
	// The agent runs as the same user; nobody else on the machine may call tools.
	if err := os.Chmod(socketPath, 0o600); err != nil {
		return err
	}

	srv := server.New(ctx, logger)
	tools.RegisterPing(srv, version)
	tools.RegisterBash(srv)
	tools.RegisterRead(srv)
	tools.RegisterWrite(srv)
	tools.RegisterEdit(srv)
	logger.Printf("toolbox: listening on %s tools=%v", socketPath, srv.Names())

	done := make(chan error, 1)
	go func() { done <- srv.Serve(listener) }()
	<-ctx.Done()
	// Closing the listener unblocks Serve's Accept, then Close waits for the
	// calls in flight so a killed bash does not outlive the process.
	_ = listener.Close()
	srv.Close()
	<-done
	_ = os.Remove(socketPath)
	logger.Printf("toolbox: stopped")
	return nil
}
