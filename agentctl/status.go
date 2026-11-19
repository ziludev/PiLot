package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type feishuHealth struct {
	Started   bool   `json:"started"`
	Ready     bool   `json:"ready"`
	DownMs    int64  `json:"downMs"`
	LastError string `json:"lastError"`
}

type bridgeHealth struct {
	Feishu *feishuHealth `json:"feishu"`
}

// status answers the one question launchd cannot: can the bot still hear Feishu? A silent bot and a
// quiet chat look identical from the outside, so the bridge reports it and this prints it first.
func status(p project) error {
	port := envValueOr(filepath.Join(p.root, ".env"), "BRIDGE_PORT", os.Getenv("BRIDGE_PORT"))
	if port == "" {
		port = "8787"
	}
	token := envValue(filepath.Join(p.root, ".env"), "AGENT_HTTP_TOKEN")
	if line := feishuLine(health(port, token)); line != "" {
		fmt.Println(line)
	}
	for _, label := range []string{labelAgent, labelGW, labelBack} {
		out, err := execPrint(label)
		if err != nil {
			fmt.Printf("== %s: not loaded\n", label)
			continue
		}
		fmt.Printf("== %s\n", label)
		for _, line := range strings.Split(out, "\n") {
			// The same four facts the shell filtered for: what launchd thinks of this job.
			if strings.Contains(line, "state = ") || strings.Contains(line, "pid = ") ||
				strings.Contains(line, "last exit code") || strings.Contains(line, "runs = ") {
				fmt.Printf("  %s\n", line)
			}
		}
	}
	return nil
}

func execPrint(label string) (string, error) {
	out, err := launchctlOutput("print", domain()+"/"+label)
	if err != nil {
		return "", err
	}
	return out, nil
}

func launchctlOutput(args ...string) (string, error) {
	out, err := command("launchctl", args...)
	if err != nil {
		return "", err
	}
	return out, nil
}

func health(port, token string) (bridgeHealth, bool) {
	client := &http.Client{Timeout: 3 * time.Second}
	request, err := http.NewRequest(http.MethodGet, "http://127.0.0.1:"+port+"/api/health", nil)
	if err != nil {
		return bridgeHealth{}, false
	}
	request.Header.Set("authorization", "Bearer "+token)
	response, err := client.Do(request)
	if err != nil {
		return bridgeHealth{}, false
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return bridgeHealth{}, false
	}
	var parsed bridgeHealth
	if err := json.Unmarshal(body, &parsed); err != nil || parsed.Feishu == nil {
		return bridgeHealth{}, false
	}
	return parsed, true
}

func feishuLine(payload bridgeHealth, ok bool) string {
	if !ok || payload.Feishu == nil {
		return ""
	}
	feishu := payload.Feishu
	switch {
	case !feishu.Started:
		return "== feishu: not started (no credentials or no owner)"
	case feishu.Ready:
		return "== feishu: ok (gateway connected)"
	case feishu.LastError != "":
		return fmt.Sprintf("== feishu: DOWN for %d min — %s", feishu.DownMs/60000, feishu.LastError)
	default:
		return fmt.Sprintf("== feishu: DOWN for %d min", feishu.DownMs/60000)
	}
}
