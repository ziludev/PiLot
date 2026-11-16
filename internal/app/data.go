package app

import (
	"context"
	"fmt"
)

// The loaders below run off the UI thread and write their result through
// apply, so a slow bridge never stalls a frame.

func (a *App) loadChanges(ctx context.Context) error {
	current := a.currentValue()
	changes, err := a.api.Changes(ctx, current)
	if err != nil {
		return fmt.Errorf("load changes: %w", err)
	}
	a.apply(func() {
		a.changes = changes
		a.invalidate()
	})
	return nil
}

func (a *App) loadGit(ctx context.Context) error {
	current := a.currentValue()
	if current == 0 {
		return nil
	}
	git, err := a.api.Git(ctx, current)
	if err != nil {
		return fmt.Errorf("load git: %w", err)
	}
	a.apply(func() {
		a.git = git
		a.invalidate()
	})
	return nil
}

func (a *App) loadModels(ctx context.Context) error {
	models, err := a.api.Models(ctx)
	if err != nil {
		return fmt.Errorf("load models: %w", err)
	}
	a.apply(func() {
		a.models = models
		a.invalidate()
	})
	return nil
}

func (a *App) loadMemory(ctx context.Context) error {
	current := a.currentValue()
	if current == 0 {
		return nil
	}
	memory, err := a.api.Memory(ctx, current)
	if err != nil {
		return fmt.Errorf("load memory: %w", err)
	}
	a.apply(func() {
		a.memory = *memory
		a.invalidate()
	})
	return nil
}

func (a *App) loadUsage(ctx context.Context, days int) error {
	daysOut, err := a.api.UsageCalendar(ctx, days)
	if err != nil {
		return fmt.Errorf("load usage: %w", err)
	}
	a.apply(func() {
		a.usageDays = daysOut
		a.invalidate()
	})
	return nil
}

// conversationIsOpen reports whether conversations carry a surface, which the
// rail needs before it can show a chat's title.
func (a *App) conversationIsOpen() bool {
	open := false
	a.apply(func() { open = a.current != 0 })
	return open
}

// loadApprovals refreshes the pending tool calls; the composer shows the ones
// for the open chat and the rail warns about the rest.
func (a *App) loadApprovals(ctx context.Context) error {
	approvals, err := a.api.Approvals(ctx)
	if err != nil {
		return fmt.Errorf("load approvals: %w", err)
	}
	a.apply(func() {
		a.approvals = approvals
		a.invalidate()
	})
	return nil
}
