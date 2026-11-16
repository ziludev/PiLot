package app

import (
	"context"
	"os/exec"
	"strings"
)

// contextBG is the context the loaders and actions start from: they must
// outlive the frame that triggered them.
func contextBG() context.Context { return context.Background() }

// control runs scripts/agentctl for one action and reports what it printed.
func (a *App) control(action string) {
	out, err := exec.Command(agentctlPath(a.cfg.Home), action).CombinedOutput()
	text := strings.TrimSpace(string(out))
	if err != nil {
		a.fail("agentctl %s: %v: %s", action, err, text)
		return
	}
	a.apply(func() {
		a.notice = text
		a.invalidate()
	})
}

// agentctlPath is the agent's control script, next to its .env.
func agentctlPath(home string) string {
	return home + "/scripts/agentctl"
}
