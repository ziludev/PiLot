package app

import (
	"image"
	"image/png"
	"os"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"pitta/internal/protocol"
	"pitta/internal/theme"
)

// snapshotData fills an App with the shapes the panels draw, so a snapshot
// shows every kind of row without a bridge.
func snapshotData(t *testing.T) *App {
	t.Helper()
	a := New(Config{})
	a.previewImage = -1
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
		a.prefs.Theme = "light"
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
	if !tt.HasText("Claude Sonnet 4") {
		t.Fatal("model picker did not draw the list")
	}

	a = snapshotData(t)
	a.modelOpen = false
	tt = ui.NewTester(a.View, 1180, 820)
	tt.Frame()
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
