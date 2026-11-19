package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"
)

func domain() string { return fmt.Sprintf("gui/%d", os.Getuid()) }

func launchctl(args ...string) error {
	return exec.Command("launchctl", args...).Run()
}

// render writes an installed plist from launchd/*.tmpl. The shell did this with sed and four
// substitutions, which quietly mangled any path containing the separator; the replacement here is
// literal, so spaces and `&` in the project path are just characters.
func (p project) render(templateName, label, destination string) error {
	source, err := os.ReadFile(p.template(templateName))
	if err != nil {
		return fmt.Errorf("missing template: %s", p.template(templateName))
	}
	node := os.Getenv("NODE_BIN")
	if node == "" {
		if node, err = exec.LookPath("node"); err != nil {
			return fmt.Errorf("node not found in PATH; set NODE_BIN=/path/to/node")
		}
	}
	text := string(source)
	for placeholder, value := range map[string]string{
		"__LABEL__":   label,
		"__PROJECT__": p.root,
		"__NODE__":    node,
		"__HOME__":    p.home,
	} {
		text = strings.ReplaceAll(text, placeholder, value)
	}
	if err := os.MkdirAll(p.agentsDir(), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(p.root, "logs"), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(destination, []byte(text), 0o644); err != nil {
		return err
	}
	fmt.Printf("rendered %s (node: %s)\n", destination, node)
	return nil
}

// load replaces whatever launchd has under this label. bootout of a label that is not loaded is
// normal, not an error.
func (p project) load(label, plist string) error {
	_ = launchctl("bootout", domain(), plist)
	if err := launchctl("bootstrap", domain(), plist); err != nil {
		return fmt.Errorf("launchctl bootstrap %s: %w", plist, err)
	}
	if err := launchctl("enable", domain()+"/"+label); err != nil {
		return fmt.Errorf("launchctl enable %s: %w", label, err)
	}
	return nil
}

func install(p project) error {
	plist := p.plistPath(labelAgent)
	if err := p.render("agent.plist.tmpl", labelAgent, plist); err != nil {
		return err
	}
	p.rotateLogs()
	if err := p.load(labelAgent, plist); err != nil {
		return err
	}
	fmt.Printf("agent installed; logs: %s\n", filepath.Join(p.root, "logs", "agent.log"))
	return installGateway(p)
}

// installGateway builds the transport half. It is not optional: the bridge has no Feishu connection
// of its own, so without this process the bot is deaf and mute.
func installGateway(p project) error {
	if !hasCommand("go") {
		return fmt.Errorf("go not found in PATH; cannot build feishu-gw")
	}
	if err := os.MkdirAll(filepath.Join(p.root, "bin"), 0o755); err != nil {
		return err
	}
	build := exec.Command("go", "build", "-o", filepath.Join(p.root, "bin", "feishu-gw"), ".")
	build.Dir = filepath.Join(p.root, "feishu-gw")
	build.Stdout = os.Stdout
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		return fmt.Errorf("go build feishu-gw: %w", err)
	}
	plist := p.plistPath(labelGW)
	if err := p.render("feishu-gw.plist.tmpl", labelGW, plist); err != nil {
		return err
	}
	if err := p.load(labelGW, plist); err != nil {
		return err
	}
	fmt.Printf("feishu-gw installed; logs: %s\n", filepath.Join(p.root, "logs", "feishu-gw.log"))
	return nil
}

func installBackup(p project) error {
	plist := p.plistPath(labelBack)
	if err := p.render("backup.plist.tmpl", labelBack, plist); err != nil {
		return err
	}
	if err := p.load(labelBack, plist); err != nil {
		return err
	}
	fmt.Printf("daily backup installed (04:10); logs: %s\n", filepath.Join(p.root, "logs", "backup.log"))
	return nil
}

func uninstall(p project) error {
	for _, label := range []string{labelGW, labelAgent, labelBack} {
		plist := p.plistPath(label)
		_ = launchctl("bootout", domain(), plist)
		os.Remove(plist)
		fmt.Printf("unloaded %s\n", label)
	}
	return nil
}

func start(p project) error {
	gwPlist := p.plistPath(labelGW)
	if _, err := os.Stat(gwPlist); err == nil {
		_ = launchctl("kickstart", domain()+"/"+labelGW)
	}
	if err := launchctl("kickstart", domain()+"/"+labelAgent); err != nil {
		return fmt.Errorf("launchctl kickstart %s: %w", labelAgent, err)
	}
	return nil
}

func stop(p project) error {
	// Gateway first: with it gone the bridge stops hearing anything, and a half-shutdown cannot
	// look like a working bot while the bridge is still being taken down.
	if _, err := os.Stat(p.plistPath(labelGW)); err == nil {
		_ = launchctl("kill", "SIGTERM", domain()+"/"+labelGW)
	}
	if err := launchctl("kill", "SIGTERM", domain()+"/"+labelAgent); err != nil {
		if err := launchctl("bootout", domain(), p.plistPath(labelAgent)); err != nil {
			return fmt.Errorf("launchctl bootout %s: %w", labelAgent, err)
		}
	}
	return nil
}

// restart never cuts a turn in half unless forced: it waits for the queue to drain, tells the next
// boot why it went away, then reloads (kickstart -k would keep the log file descriptors).
func restart(p project, force bool) error {
	reason := "手动重启"
	if force {
		reason = "强制重启（--force），手上回合被切断"
	} else if !p.waitIdle() {
		return exitWith(1, "not restarting")
	}
	if err := p.noteRestart("manual", reason); err != nil {
		return err
	}
	_ = launchctl("bootout", domain(), p.plistPath(labelAgent))
	p.rotateLogs()
	if err := p.load(labelAgent, p.plistPath(labelAgent)); err != nil {
		return err
	}
	if _, err := os.Stat(p.plistPath(labelGW)); err == nil {
		_ = launchctl("kickstart", "-k", domain()+"/"+labelGW)
	}
	return nil
}

// restartLater queues the restart and returns: this process is the agent itself, so it goes away
// with the bootout and nothing here can watch the turn end.
func restartLater(p project) error {
	if err := p.noteRestart("queued", "我排队重启，等手上的回合跑完再加载新代码"); err != nil {
		return err
	}
	plist := p.plistPath(labelLater)
	if err := p.renderLater(plist); err != nil {
		return err
	}
	_ = launchctl("bootout", domain(), plist)
	if err := launchctl("bootstrap", domain(), plist); err != nil {
		return fmt.Errorf("launchctl bootstrap %s: %w", plist, err)
	}
	_ = launchctl("enable", domain()+"/"+labelLater)
	fmt.Println("queued: restarts once the running turn settles (log: logs/restart-later.log)")
	return nil
}

// renderLater writes the one-shot job. KeepAlive must stay false: on 2026-11-18 a restart submitted
// through `launchctl submit` was re-run every 25 s (20 s sleep + bootout + bootstrap), which restarted
// the agent 26 times in eleven minutes and killed the turn that asked for it each time.
func (p project) renderLater(destination string) error {
	const body = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>{{.Label}}</string>
  <key>ProgramArguments</key>
  <array>
    <string>/bin/bash</string>
    <string>{{.Script}}</string>
    <string>restart-once</string>
  </array>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><false/>
  <key>AbandonProcessGroup</key><true/>
  <key>StandardOutPath</key><string>{{.Log}}</string>
  <key>StandardErrorPath</key><string>{{.Log}}</string>
</dict>
</plist>
`
	parsed, err := template.New("later").Parse(body)
	if err != nil {
		return err
	}
	var rendered bytes.Buffer
	if err := parsed.Execute(&rendered, map[string]string{
		"Label":  labelLater,
		"Script": filepath.Join(p.root, "scripts", "agentctl"),
		"Log":    filepath.Join(p.root, "logs", "restart-later.log"),
	}); err != nil {
		return err
	}
	if err := os.MkdirAll(p.agentsDir(), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(p.root, "logs"), 0o755); err != nil {
		return err
	}
	return os.WriteFile(destination, rendered.Bytes(), 0o644)
}

// restartOnce is what launchd runs, once. It must not unload its own label first: booting out the
// job that is running kills this very process, which is how the first version waited for nothing.
// One idle window often loses the race (other chats keep starting turns), so keep knocking.
func restartOnce(p project) error {
	attempts := 12
	if raw := os.Getenv("RESTART_ATTEMPTS"); raw != "" {
		if value := argInt([]string{raw}, 12); value > 0 {
			attempts = value
		}
	}
	for attempt := 1; !p.waitIdle(); attempt++ {
		if attempt >= attempts {
			fmt.Printf("left the agent alone: still mid-turn after %d attempts\n", attempts)
			return exitWith(1, "still mid-turn")
		}
		fmt.Printf("attempt %d lost the race; trying again in 60s\n", attempt)
		sleepSeconds(60)
	}
	// Refresh the note: the fire time is now, the reason and queue time stay as queued.
	if err := p.noteRestart("planned", ""); err != nil {
		return err
	}
	_ = launchctl("bootout", domain(), p.plistPath(labelAgent))
	p.rotateLogs()
	if err := p.load(labelAgent, p.plistPath(labelAgent)); err != nil {
		return err
	}
	fmt.Printf("restarted at %s\n", timestamp())
	// Last, so the next restart-later starts from a clean label.
	_ = launchctl("bootout", domain(), p.plistPath(labelLater))
	return nil
}
