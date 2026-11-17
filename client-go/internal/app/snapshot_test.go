package app

import (
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"pitta/internal/api"
	"pitta/internal/protocol"
	"pitta/internal/theme"
)

// snapshotData fills an App with the shapes the panels draw, so a snapshot
// shows every kind of row without a bridge.
func snapshotData(t *testing.T) *App {
	t.Helper()
	a := New(Config{})
	a.previewImage = -1
	// The pictures must not depend on the preference file another test left
	// behind; each snapshot test overrides the theme it wants.
	// Start from a known preference set: New() reads ~/.pitta/ui.json, and
	// another test's pins, order or recents would otherwise leak in here.
	a.prefs = prefs{Theme: "light"}
	a.railCursor = 0
	a.conn = ConnOpen
	a.current = 11
	a.surfaces = []protocol.Surface{
		{SurfaceID: "client:11", ConversationID: 11, Kind: "client", Title: "why is the sky blue", Cost: 0.5, Tokens: 12000, ProjectID: "p1"},
		{SurfaceID: "feishu:12", ConversationID: 12, Kind: "feishu", Title: "Feishu: deploy", Cost: 2.25, ProjectID: "p1"},
		{SurfaceID: "client:13", ConversationID: 13, Kind: "client", Cost: 0.125, ProjectID: "p1"},
	}
	a.projects = []protocol.Project{{ID: "p1", Name: "workspace", Path: "/Users/example/workspace", Builtin: true}}
	a.project = "p1"
	a.tasks = []protocol.Task{{ID: 5, Title: "app.digest", State: "owner"}}
	a.model = &protocol.ModelRef{Provider: "sitin-deepseek", ModelID: "deepseek-flash"}
	a.thinking = "high"
	a.models = []protocol.ModelEntry{
		{ID: "claude-sonnet-4", Name: "Claude Sonnet 4", Provider: "anthropic", ContextWindow: 200000,
			Cost: protocol.ModelCost{Input: 3, Output: 15}, ThinkingLevels: []string{"off", "low", "high"}},
		{ID: "deepseek-flash", Name: "DeepSeek Flash", Provider: "sitin-deepseek", ContextWindow: 128000},
	}
	a.modelsFav = []string{"anthropic/claude-sonnet-4"}
	a.approvals = []protocol.PendingApproval{{
		CallID: "call-1", GrantKey: "bash:rm", Tool: "bash",
		Summary: "rm -rf ./build && go build ./...", Reason: "destructive command", CreatedAt: time.Now().UnixMilli() - 45_000,
	}}
	a.entries = append(a.entries, turnEntries(1, "why is the sky blue", "Rayleigh scattering: shorter wavelengths scatter more.")...)
	a.entries = append(a.entries, turnEntries(3, "and at sunset?", "The path through air is longer, so blue is scattered away.")...)
	a.changes = &protocol.Changes{
		Repo:   true,
		Branch: "main",
		Files:  []protocol.FileChange{{Path: "main.go", Status: "modified", Added: 3, Removed: 1}},
		Diff:   "diff --git a/main.go b/main.go\n--- a/main.go\n+++ b/main.go\n@@ -1,2 +1,3 @@\n package main\n-old line\n+new line\n+another\n",
	}
	a.git = &protocol.Git{
		Worktree: "/Users/example/workspace", Branch: "main", Base: "origin/main",
		Commits: []protocol.Commit{{Hash: "abc1234", Short: "abc1234", Subject: "fix the parser", Date: "2h ago"}},
	}
	a.memory = protocol.Memory{
		Global: []protocol.Note{{At: "2h", Text: "speak Chinese"}},
		Work:   []protocol.Note{{At: "1h", Text: "repo uses pgx"}},
	}
	a.access = &protocol.AccessView{Chats: []protocol.AccessRow{{ChatID: "c1", Level: "owner", Title: "owner"}}}
	a.audit = []protocol.AuditEvent{{At: 1763300000, Kind: "grant", Detail: "bash allowed"}}
	a.usageDays = []protocol.UsageDay{{Date: "2026-11-16", Cost: 1.25, Tokens: 12000}}
	a.review = []ReviewNote{{Path: "main.go", Line: "2", Note: "this line leaks"}}
	return a
}

// TestSnapshotMainWindow draws the whole window in both themes and checks the
// palettes actually land on screen, then leaves the pngs for a person to look
// at.
func TestSnapshotMainWindow(t *testing.T) {
	for _, dark := range []bool{false, true} {
		a := snapshotData(t)
		// Start from a known preference set: New() reads ~/.pitta/ui.json, and
		// another test's pins, order or recents would otherwise leak in here.
		a.prefs = prefs{Theme: "light"}
		if dark {
			a.prefs.Theme = "dark"
		}
		tt := ui.NewTester(a.View, 1180, 820)
		tt.SetDark(dark)
		tt.Frame()
		image := tt.Image()
		want := theme.Light()
		if dark {
			want = theme.Dark()
		}
		if countColor(image, want.Background) < 1000 {
			t.Fatalf("dark=%v: background %v barely on screen", dark, want.Background)
		}
		if countColor(image, want.Primary) == 0 {
			t.Fatalf("dark=%v: accent %v missing", dark, want.Primary)
		}
		// The pending approval was created 45s ago: if the units ever slip back to
		// seconds the bar would print a nine-digit wait instead.
		assertHasText(t, tt, "45s")
		name := "/tmp/pitta-app-light.png"
		if dark {
			name = "/tmp/pitta-app-dark.png"
		}
		writePNG(t, name, image)
	}
}

// TestSnapshotSidePanels draws each side tab with data, so the panels are
// covered too.
func TestSnapshotSidePanels(t *testing.T) {
	tabs := []SideTab{SideChanges, SideMemory, SideAccess, SideUsage}
	names := []string{"changes", "memory", "access", "usage"}
	for index, tab := range tabs {
		a := snapshotData(t)
		a.sideOpen = true
		a.sideTab = tab
		tt := ui.NewTester(a.View, 1180, 820)
		tt.Frame()
		writePNG(t, "/tmp/pitta-app-side-"+names[index]+".png", tt.Image())
	}
}

// TestSnapshotApprovalAndModels draws the approval bar and the model picker
// open, the two states a first frame cannot reach from data alone.
func TestSnapshotApprovalAndModels(t *testing.T) {
	a := snapshotData(t)
	a.modelOpen = true
	tt := ui.NewTester(a.View, 1180, 820)
	tt.Frame()
	writePNG(t, "/tmp/pitta-app-modelpicker.png", tt.Image())
	// Every provider but the chat's own starts folded, as the phase bar does.
	if !tt.HasText("DeepSeek Flash") {
		t.Fatal("model picker did not draw the list")
	}
	if tt.HasText("Claude Sonnet 4") {
		t.Fatal("another provider's group should start folded")
	}
	// Opening it draws that provider's rows.
	tt.Click("anthropic")
	tt.Frame()
	if !tt.HasText("Claude Sonnet 4") {
		t.Fatal("unfolding a provider did not draw its models")
	}

	a = snapshotData(t)
	a.modelOpen = false
	// Two pending calls, so this frame differs from the plain main window: an
	// approval snapshot that is byte-identical to the light one proves nothing.
	a.approvals = append(a.approvals, protocol.PendingApproval{
		CallID: "call_2", GrantKey: "grant_2", Tool: "edit", Reason: "写入仓库文件",
		Summary: "internal/app/rail.go", SurfaceID: "client:abc", CreatedAt: time.Now().UnixMilli() - 12_000,
	})
	tt = ui.NewTester(a.View, 1180, 820)
	tt.Frame()
	if !tt.HasText("internal/app/rail.go") {
		t.Fatalf("second approval did not draw: %q", tt.Texts())
	}
	writePNG(t, "/tmp/pitta-app-approval.png", tt.Image())
	if !tt.HasText("destructive command") {
		t.Fatal("approval bar did not draw")
	}
}

// assertHasText fails when the frame drew no text containing want.
func assertHasText(t *testing.T, tt *ui.Tester, want string) {
	t.Helper()
	if tt.HasText(want) {
		return
	}
	t.Fatalf("no text containing %q; drew %v", want, tt.Texts())
}

// countColor counts the pixels of one exact color, which is how a snapshot
// proves a token reached the screen.
func countColor(img *image.RGBA, want ui.Color) int {
	count := 0
	bounds := img.Bounds()
	for x := bounds.Min.X; x < bounds.Max.X; x++ {
		for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
			r, g, b, a := img.At(x, y).RGBA()
			if nearColor(uint8(r>>8), want.R) && nearColor(uint8(g>>8), want.G) &&
				nearColor(uint8(b>>8), want.B) && nearColor(uint8(a>>8), want.A) {
				count++
			}
		}
	}
	return count
}

// writePNG saves a snapshot for review.
// snapshotDir keeps an ordinary `go test ./...` from rewriting files under
// version control: set PITTA_SNAPSHOT_DIR=../../docs/snapshots when the recorded
// copies are what should be refreshed.
func snapshotDir() string {
	if dir := os.Getenv("PITTA_SNAPSHOT_DIR"); dir != "" {
		return dir
	}
	return os.TempDir()
}

func writePNG(t *testing.T, name string, img *image.RGBA) {
	t.Helper()
	file, err := os.Create(name)
	if err != nil {
		t.Fatalf("create %s: %v", name, err)
	}
	defer file.Close()
	if err := png.Encode(file, img); err != nil {
		t.Fatalf("encode %s: %v", name, err)
	}
}

// nearColor tolerates the renderer's 1-2 value rounding.
func nearColor(a, b uint8) bool {
	d := int(a) - int(b)
	return d >= -2 && d <= 2
}

// TestSnapshotChatMenu renders the chat actions to docs/snapshots so a reader
// can see the labels without opening the app; TestChatMenuDrawsLabels is the
// assertion that they are labels and not keys.
func TestSnapshotChatMenu(t *testing.T) {
	a := New(Config{})
	a.previewImage = -1
	// The pictures must not depend on the preference file another test left
	// behind; each snapshot test overrides the theme it wants.
	// Start from a known preference set: New() reads ~/.pitta/ui.json, and
	// another test's pins, order or recents would otherwise leak in here.
	a.prefs = prefs{Theme: "light"}
	a.railCursor = 0
	a.current = 7
	a.surfaces = []protocol.Surface{{ConversationID: 7, SurfaceID: "s7", Title: "想摸鱼"}}
	a.menu.open = true
	a.menu.title = "想摸鱼"

	for _, c := range []struct {
		name string
		tok  theme.Tokens
		dark bool
	}{{"light", theme.Light(), false}, {"dark", theme.Dark(), true}} {
		tt := ui.NewTester(func(ctx *ui.Context) {
			theme.Apply(ctx, c.tok)
			a.menu.open = true
			a.chatMenuDialog(ctx, c.tok)
		}, 480, 260)
		tt.SetDark(c.dark)
		tt.Frame()
		writePNG(t, filepath.Join(snapshotDir(), "chat-menu-"+c.name+".png"), tt.Image())
	}
}

// TestSnapshotRail renders the rail with the cross-conversation hits open, so a
// reader can see what /api/search returns without asking the bridge again.
func TestSnapshotRail(t *testing.T) {
	a := snapshotData(t)
	a.searchHits = []protocol.SearchHit{
		{ConversationID: 13, EntryID: 91, Kind: "pi.assistant", Snippet: "the parser drops trailing commas"},
		{ConversationID: 12, EntryID: 88, Kind: "pi.user", Snippet: "deploy the feishu bridge again"},
		{ConversationID: 11, EntryID: 74, Kind: "pi.tool-result", Snippet: "ok  pitta/internal/app  0.83s"},
	}
	a.filter = "parser"

	for _, c := range []struct {
		name string
		tok  theme.Tokens
		dark bool
	}{{"light", theme.Light(), false}, {"dark", theme.Dark(), true}} {
		tt := ui.NewTester(func(ctx *ui.Context) {
			theme.Apply(ctx, c.tok)
			a.railWidth = 260
			a.rail(ctx)
		}, 260, 560)
		tt.SetDark(c.dark)
		tt.Frame()
		writePNG(t, filepath.Join(snapshotDir(), "rail-"+c.name+".png"), tt.Image())
	}
}

// TestSnapshotSettings shows the ⌘, panel: appearance, the window's transparency,
// the daemon's controls and the key list.
func TestSnapshotSettings(t *testing.T) {
	a := snapshotData(t)
	a.settingsOpen = true
	a.prefs.Theme = "light"
	a.prefs.Opacity = 0.85
	tt := ui.NewTester(a.View, 1100, 800)
	tt.Frame()
	writePNG(t, filepath.Join(snapshotDir(), "pitta-settings.png"), tt.Image())
}

// TestSnapshotRailStatus shows the marks a row keeps for the work its chat has,
// at the width where the five views of the filter have to fit.
func TestSnapshotRailStatus(t *testing.T) {
	a := snapshotData(t)
	// One of each: a task that waits, one that runs, one parked until its clock,
	// and an answer nobody read.
	a.surfaces = append(a.surfaces, protocol.Surface{
		SurfaceID: "client:14", ConversationID: 14, Kind: "client", Title: "nightly digest", ProjectID: "p1",
	})
	a.tasks = []protocol.Task{
		{ID: 5, ConversationID: 11, State: "blocked"},
		{ID: 6, ConversationID: 12, State: "running"},
		{ID: 7, ConversationID: 14, State: "sleeping"},
	}
	a.unread = map[int]bool{12: true, 13: true}
	tt := ui.NewTester(func(ctx *ui.Context) {
		theme.Apply(ctx, theme.Light())
		a.railWidth = 210
		a.rail(ctx)
	}, 210, 420)
	tt.Frame()
	writePNG(t, filepath.Join(snapshotDir(), "rail-status.png"), tt.Image())
}

// TestSnapshotStepExpanded opens a tool step holding a patch so the accordion
// can be looked at mid-way: it writes the frame before the click, one 175 ms
// into the 350 ms growth, and the resting one.
func TestSnapshotStepExpanded(t *testing.T) {
	patch := "diff --git a/a.go b/a.go\n--- a/a.go\n+++ b/a.go\n@@ -1 +1 @@\n-old line\n+new line"
	entries := []protocol.Entry{
		{ID: 1, Kind: "pi.user", Model: rawModel(map[string]any{"role": "user", "content": "fix it"})},
		{ID: 2, Kind: "pi.assistant", Model: rawModel(map[string]any{
			"role": "assistant", "content": []any{map[string]any{
				"type": "toolCall", "id": "call_1", "name": "edit", "arguments": map[string]any{"path": "a.go"},
			}},
		})},
		{ID: 3, Kind: "pi.tool-result", Model: rawModel(map[string]any{
			"role": "toolResult", "toolCallId": "call_1", "toolName": "edit",
			"content": []any{map[string]any{"type": "text", "text": patch}},
		})},
	}
	a := transcriptApp(entries...)
	tt := ui.NewTester(func(c *ui.Context) { theme.Apply(c, theme.Light()); a.transcript(c) }, 800, 400)
	tt.Frame()
	row, ok := tt.Find("edit · a.go")
	if !ok {
		t.Fatalf("no step line; texts=%q", tt.Texts())
	}
	writePNG(t, filepath.Join(os.TempDir(), "pitta-step-folded.png"), tt.Image())
	tt.ClickAt(row.X+2, row.Y+2)
	tt.Frame()
	tt.Advance(175 * time.Millisecond)
	tt.Frame()
	writePNG(t, filepath.Join(os.TempDir(), "pitta-step-midway.png"), tt.Image())
	tt.Advance(300 * time.Millisecond)
	tt.Frame()
	writePNG(t, filepath.Join(os.TempDir(), "pitta-step-open.png"), tt.Image())
}

// TestRailRowLiftsUnderThePointer is the hover lift: a row under the pointer
// rises, and the rows around it stay where they are. Bounds are read a frame
// after the pointer moves, since the lift is animated over 300 ms.
func TestRailRowLiftsUnderThePointer(t *testing.T) {
	a := snapshotData(t)
	tt := ui.NewTester(func(c *ui.Context) { theme.Apply(c, theme.Light()); a.rail(c) }, 260, 560)
	tt.Frame()
	row, ok := tt.Find("New chat #13")
	if !ok {
		t.Fatalf("no row to hover; texts=%q", tt.Texts())
	}
	rest := row.Y
	tt.Move(row.X+40, row.Y+10)
	for i := 0; i < 12; i++ {
		tt.Frame()
		tt.Advance(40 * time.Millisecond)
	}
	hovered, ok := tt.Find("New chat #13")
	if !ok {
		t.Fatalf("the row went away; texts=%q", tt.Texts())
	}
	if hovered.Y >= rest {
		t.Fatalf("hovered row y = %v, at rest %v: it did not lift", hovered.Y, rest)
	}
}

// TestSnapshotTurn records one turn with tool steps, so the rounded timeline
// rail and the answer block can be compared between themes.
func TestSnapshotTurn(t *testing.T) {
	a := snapshotData(t)
	a.entries = turnEntries(4242, "why is the sky blue", "Rayleigh scattering favours short wavelengths.")
	a.waits = nil
	a.approvals = nil
	a.live = nil
	for _, dark := range []bool{false, true} {
		// Pin the preference: the picture must not depend on the preference
		// file another test happened to leave behind.
		// Start from a known preference set: New() reads ~/.pitta/ui.json, and
		// another test's pins, order or recents would otherwise leak in here.
		a.prefs = prefs{Theme: "light"}
		if dark {
			a.prefs.Theme = "dark"
		}
		tt := ui.NewTester(a.View, 1180, 720)
		tt.SetDark(dark)
		tt.Frame()
		tt.Frame()
		name := "turn-light"
		if dark {
			name = "turn-dark"
		}
		path := filepath.Join(snapshotDir(), name+".png")
		writePNG(t, path, tt.Image())
		if stat, err := os.Stat(path); err != nil || stat.Size() < 4000 {
			t.Fatalf("%s: %v (%v bytes)", name, err, stat)
		}
	}
}

// TestSnapshotLogViewer renders the log dialog over the window, so the shape a
// reader gets when they press the log button is reviewable without the app.
func TestSnapshotLogViewer(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PI_DURABLE_HOME", home)
	logs := filepath.Join(home, "logs")
	if err := os.MkdirAll(logs, 0o755); err != nil {
		t.Fatalf("mkdir logs: %v", err)
	}
	body := strings.Join([]string{
		"[2026-11-17 13:20:01] [toolbox] 2026/11/17 13:20:01 ready pid=94054 version=0.1.0 tools=[bash edit read write] in 54ms",
		"[2026-11-17 13:20:09] [feishu] turn start chat=oc_1c22 conversationId=40508",
		"[2026-11-17 13:20:11] [toolbox] 2026/11/17 13:20:11 bash: tool=bash exit=0 spill=false timeout=false took=19ms",
		"[2026-11-17 13:20:11] [toolbox] 2026/11/17 13:20:11 read: path=/Users/alexwang/learn/pi-durable-agent/workspace/blank-probe.txt bytes=7 image=false truncated=false took=0s",
		"[2026-11-17 13:20:12] [feishu] settled {\"status\":\"done\",\"seconds\":3}",
		"[2026-11-17 13:20:14] [error] feishu: could not patch approval card: context deadline exceeded",
	}, "\n")
	if err := os.WriteFile(filepath.Join(logs, "agent.log"), []byte(body+"\n"), 0o644); err != nil {
		t.Fatalf("write log: %v", err)
	}
	a := snapshotData(t)
	a.prefs = prefs{Theme: "light"}
	a.toggleLogs()
	defer a.toggleLogs()

	tt := ui.NewTester(a.View, 1180, 820)
	tt.Frame()
	assertHasText(t, tt, "bash: tool=bash exit=0")
	writePNG(t, filepath.Join(snapshotDir(), "pitta-logs.png"), tt.Image())
}

// TestSnapshotToolRunSummary draws a run of three calls with one failure, and a
// fourth call whose answer is longer than a screenful. The folded line has to say
// how many failed and how long the run took, the run has to open by itself, and the
// answer has to say how many lines it is holding back.
func TestSnapshotToolRunSummary(t *testing.T) {
	lines := make([]string, 40)
	for i := range lines {
		lines[i] = "log line " + strconv.Itoa(i+1)
	}
	call := func(entryID int, at int64, name, callID string) protocol.Entry {
		return protocol.Entry{ID: entryID, Kind: "pi.assistant", Model: rawModel(map[string]any{
			"role": "assistant", "timestamp": float64(at),
			"content": []any{map[string]any{"type": "toolCall", "id": callID, "name": name, "arguments": map[string]any{"command": "go test ./..."}}},
		})}
	}
	result := func(entryID int, at int64, callID, text string, isError bool) protocol.Entry {
		message := map[string]any{
			"role": "toolResult", "toolCallId": callID, "toolName": "bash", "timestamp": float64(at),
			"content": []any{map[string]any{"type": "text", "text": text}},
		}
		if isError {
			message["isError"] = true
		}
		return protocol.Entry{ID: entryID, Kind: "pi.tool-result", Model: rawModel(message)}
	}
	entries := []protocol.Entry{
		{ID: 1, Kind: "pi.user", Model: rawModel(map[string]any{"role": "user", "content": "跑一下测试"})},
		call(2, 1_000, "bash", "c1"), result(3, 4_200, "c1", "ok", false),
		call(4, 4_200, "bash", "c2"), result(5, 4_600, "c2", "FAIL: TestThing", true),
		call(6, 4_600, "bash", "c3"), result(7, 13_400, "c3", strings.Join(lines, "\n"), false),
	}
	a := transcriptApp(entries...)
	a.stepOpen[6] = true
	tt := ui.NewTester(func(c *ui.Context) { theme.Apply(c, theme.Light()); a.transcript(c) }, 820, 700)
	tt.Frame()
	for _, want := range []string{"3 tool calls", "1 failed", "12s", "28 more lines", "Show all"} {
		if !tt.HasText(want) {
			t.Errorf("missing %q; texts=%q", want, strings.Join(tt.Texts(), " | "))
		}
	}
	writePNG(t, "/tmp/pitta-tool-run.png", tt.Image())
}

// TestSnapshotDelegationRow draws a delegate call, a parallel batch that names
// each child's conversation, and an older batch that only names roles. The reader
// has to see who did the work without opening the child's answer, and get into
// the conversations that are known.
func TestSnapshotDelegationRow(t *testing.T) {
	call := func(entryID int, at int64, name, callID string, args map[string]any) protocol.Entry {
		return protocol.Entry{ID: entryID, Kind: "pi.assistant", Model: rawModel(map[string]any{
			"role": "assistant", "timestamp": float64(at),
			"content": []any{map[string]any{"type": "toolCall", "id": callID, "name": name, "arguments": args}},
		})}
	}
	answered := func(entryID int, at int64, callID, name string, details map[string]any, text string) protocol.Entry {
		return protocol.Entry{ID: entryID, Kind: "pi.tool-result", Model: rawModel(map[string]any{
			"role": "toolResult", "toolCallId": callID, "toolName": name, "timestamp": float64(at),
			"details": details,
			"content": []any{map[string]any{"type": "text", "text": text}},
		})}
	}
	entries := []protocol.Entry{
		{ID: 1, Kind: "pi.user", Model: rawModel(map[string]any{"role": "user", "content": "出个方案再实现"})},
		call(2, 1_000, "delegate", "c1", map[string]any{"role": "designer", "task": "写设计文档"}),
		answered(3, 22_000, "c1", "delegate", map[string]any{"conversationId": 475, "role": "designer"},
			"【designer 的产出】\n单文件 todo.py，argparse 分发……"),
		call(4, 22_000, "delegate_parallel", "c2", map[string]any{"tasks": `[{"role":"implementer"},{"role":"reviewer"}]`}),
		answered(5, 61_000, "c2", "delegate_parallel", map[string]any{
			"roles": []any{"implementer", "reviewer"}, "failed": float64(1),
			"children": []any{
				map[string]any{"role": "implementer", "conversationId": float64(477)},
				map[string]any{"role": "reviewer", "conversationId": float64(478), "failed": true},
			},
		}, "【implementer】\n写完了\n\n---\n\n【reviewer】失败：timeout"),
		call(6, 61_000, "delegate_parallel", "c3", map[string]any{"tasks": `[{"role":"planner"},{"role":"tester"}]`}),
		answered(7, 90_000, "c3", "delegate_parallel", map[string]any{"roles": []any{"planner", "tester"}, "failed": float64(1)},
			"【planner】\n拆好了\n\n---\n\n【tester】失败：timeout"),
	}
	a := transcriptApp(entries...)
	tt := ui.NewTester(func(c *ui.Context) { theme.Apply(c, theme.Light()); a.transcript(c) }, 820, 560)
	tt.Frame()
	all := strings.Join(tt.Texts(), " | ")
	for _, want := range []string{"→ designer", "→ implementer", "→ reviewer", "1 failed", "→ planner / tester"} {
		if !tt.HasText(want) {
			t.Errorf("missing %q; texts=%q", want, all)
		}
	}
	// The single delegate and both named parallel children open; the older batch
	// knows no conversations, so it offers no way in.
	if got := strings.Count(all, "Open chat"); got != 3 {
		t.Errorf("%d ways into a child, want 3; texts=%q", got, all)
	}
	writePNG(t, "/tmp/pitta-delegation.png", tt.Image())
}

// TestOpenChildChatPrefersASecondWindow keeps the parent chat where it is: a
// child conversation opens beside it, not over it.
func TestOpenChildChatPrefersASecondWindow(t *testing.T) {
	saved := windowOpener
	opened := 0
	windowOpener = func(_ Config, _ *api.Client, conversationID int) { opened = conversationID }
	defer func() { windowOpener = saved }()

	a := New(Config{})
	a.current = 12
	a.openChildChat(475)
	if opened != 475 {
		t.Fatalf("opened %d, want 475", opened)
	}
	if a.current != 12 {
		t.Fatalf("current chat moved to %d; a child opens in its own window", a.current)
	}
}
