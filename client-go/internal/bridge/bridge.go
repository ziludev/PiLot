// Package bridge holds the two conveniences the agent's .env and checkout
// offer the window: where its HTTP/WS bridge listens, and how to run
// scripts/agentctl.
package bridge

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// defaultHomeSuffix is where the agent checkout usually sits under $HOME;
// PI_DURABLE_HOME overrides the whole path.
const defaultHomeSuffix = "learn/pi-durable-agent"

// Home is the agent checkout.
func Home() string {
	if home := os.Getenv("PI_DURABLE_HOME"); home != "" {
		return home
	}
	user, err := os.UserHomeDir()
	if err != nil {
		return defaultHomeSuffix
	}
	return filepath.Join(user, defaultHomeSuffix)
}

// EnvValue reads one key from the environment or the agent's .env, without
// loading the whole file.
func EnvValue(key string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	data, err := os.ReadFile(filepath.Join(Home(), ".env"))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name, value, found := strings.Cut(line, "=")
		if !found || strings.TrimSpace(name) != key {
			continue
		}
		return strings.Trim(strings.TrimSpace(value), `"'`)
	}
	return ""
}

// BaseURL is the bridge the window talks to.
func BaseURL() string {
	port := EnvValue("AGENT_HTTP_PORT")
	if port == "" {
		port = "8787"
	}
	host := EnvValue("AGENT_HTTP_HOST")
	if host == "" || host == "0.0.0.0" {
		host = "127.0.0.1"
	}
	return "http://" + host + ":" + port
}

// Token is the bridge token; empty means the bridge has auth disabled.
func Token() string { return EnvValue("AGENT_HTTP_TOKEN") }

// Agentctl runs scripts/agentctl with action and returns its combined output.
func Agentctl(action string) string {
	switch action {
	case "start", "stop", "restart", "backup", "status":
	default:
		return "unsupported action: " + action
	}
	cmd := exec.Command(filepath.Join(Home(), "scripts", "agentctl"), action)
	cmd.Dir = Home()
	out, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(out))
	if err != nil {
		return text + "\n" + err.Error()
	}
	return text
}

// OpenLogs reveals the agent log directory in Finder.
func OpenLogs() string {
	logs := filepath.Join(Home(), "logs")
	out, err := exec.Command("open", logs).CombinedOutput()
	if err != nil {
		return "open failed: " + err.Error() + " " + strings.TrimSpace(string(out))
	}
	return logs
}
