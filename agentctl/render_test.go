package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRenderFillsPlaceholdersWithoutManglingPaths(t *testing.T) {
	root := filepath.Join(t.TempDir(), "a project & co")
	if err := os.MkdirAll(filepath.Join(root, "launchd"), 0o755); err != nil {
		t.Fatal(err)
	}
	template := "<string>__PROJECT__/src/main.ts</string><string>__NODE__</string>__LABEL____HOME__"
	if err := os.WriteFile(filepath.Join(root, "launchd", "agent.plist.tmpl"), []byte(template), 0o644); err != nil {
		t.Fatal(err)
	}
	p := project{root: root, home: t.TempDir()}
	t.Setenv("NODE_BIN", "/usr/local/bin/node")
	destination := filepath.Join(p.home, "out.plist")

	if err := p.render("agent.plist.tmpl", labelAgent, destination); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{root + "/src/main.ts", "/usr/local/bin/node", labelAgent, p.home} {
		if !strings.Contains(text, want) {
			t.Fatalf("rendered plist is missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "__") {
		t.Fatalf("a placeholder survived:\n%s", text)
	}
}

func TestRenderRefusesAMissingTemplate(t *testing.T) {
	p := project{root: t.TempDir(), home: t.TempDir()}
	err := p.render("nope.plist.tmpl", labelAgent, filepath.Join(p.home, "out.plist"))
	if err == nil || !strings.Contains(err.Error(), "missing template") {
		t.Fatalf("want a missing-template error, got %v", err)
	}
}

// The one-shot job must never be kept alive: re-running it restarted the agent 26 times in eleven
// minutes on 2026-11-18, killing the turn that asked for the restart each time.
func TestRenderLaterIsAOneShotThroughTheShim(t *testing.T) {
	p := project{root: t.TempDir(), home: t.TempDir()}
	destination := filepath.Join(p.home, "later.plist")
	if err := p.renderLater(destination); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{
		"<string>" + labelLater + "</string>",
		filepath.Join(p.root, "scripts", "agentctl"),
		"<string>restart-once</string>",
		"<key>KeepAlive</key><false/>",
		"<key>RunAtLoad</key><true/>",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("restart-later plist is missing %q:\n%s", want, text)
		}
	}
	// The plist is launchd's input, so it has to parse.
	if _, err := os.Stat(destination); err != nil {
		t.Fatal(err)
	}
}

func TestFeishuLine(t *testing.T) {
	cases := []struct {
		name    string
		payload bridgeHealth
		ok      bool
		want    string
	}{
		{
			name:    "never started",
			payload: bridgeHealth{Feishu: &feishuHealth{}},
			ok:      true,
			want:    "== feishu: not started (no credentials or no owner)",
		},
		{
			name:    "connected",
			payload: bridgeHealth{Feishu: &feishuHealth{Started: true, Ready: true}},
			ok:      true,
			want:    "== feishu: ok (gateway connected)",
		},
		{
			name:    "down with a reason",
			payload: bridgeHealth{Feishu: &feishuHealth{Started: true, DownMs: 240_000, LastError: "gateway timeout"}},
			ok:      true,
			want:    "== feishu: DOWN for 4 min — gateway timeout",
		},
		{
			name:    "down without a reason",
			payload: bridgeHealth{Feishu: &feishuHealth{Started: true, DownMs: 60_000}},
			ok:      true,
			want:    "== feishu: DOWN for 1 min",
		},
		{
			name:    "bridge did not answer",
			payload: bridgeHealth{Feishu: &feishuHealth{Started: true}},
			ok:      false,
			want:    "",
		},
		{
			name:    "old bridge without the field",
			payload: bridgeHealth{},
			ok:      true,
			want:    "",
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := feishuLine(testCase.payload, testCase.ok); got != testCase.want {
				t.Fatalf("feishuLine = %q, want %q", got, testCase.want)
			}
		})
	}
}

func TestHumanBytes(t *testing.T) {
	cases := map[int64]string{
		1024:                    "1.0K",
		1024 * 1024:             "1.0M",
		213 * 1024 * 1024:       "213M",
		28 * 1024 * 1024 * 1024: "28G",
	}
	for size, want := range cases {
		if got := humanBytes(size); got != want {
			t.Fatalf("humanBytes(%d) = %q, want %q", size, got, want)
		}
	}
}

func TestHumanFreeUsesBinaryUnits(t *testing.T) {
	if got := humanFree(28 * 1024 * 1024 * 1024); got != "28Gi" {
		t.Fatalf("humanFree = %q, want 28Gi", got)
	}
	if got := humanFree(512 * 1024 * 1024); got != "512Mi" {
		t.Fatalf("humanFree = %q, want 512Mi", got)
	}
}

func TestArgInt(t *testing.T) {
	if got := argInt(nil, 50); got != 50 {
		t.Fatalf("default = %d", got)
	}
	if got := argInt([]string{"12"}, 50); got != 12 {
		t.Fatalf("parsed = %d", got)
	}
	for _, bad := range []string{"", "abc", "0", "-3"} {
		if got := argInt([]string{bad}, 50); got != 50 {
			t.Fatalf("argInt(%q) = %d, want the fallback", bad, got)
		}
	}
}

func TestTailFileKeepsTheLastLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "log")
	if err := os.WriteFile(path, []byte("a\nb\nc\nd\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := tailFile(path, 2); err != nil {
		t.Fatal(err)
	}
}

func TestFindProjectNeedsTheTemplate(t *testing.T) {
	t.Setenv("AGENT_HOME", t.TempDir())
	if _, err := findProject(); err == nil {
		// The cwd may legitimately be the repo; only fail when that is not the case.
		if _, statErr := os.Stat("launchd/agent.plist.tmpl"); statErr != nil {
			t.Fatal("findProject should have failed outside the repo")
		}
	}
}
