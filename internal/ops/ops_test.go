package ops

import (
	"encoding/json"
	"image/png"
	"os"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"pitta/internal/i18n"
	"pitta/internal/protocol"
	"pitta/internal/theme"
)

// frame paints one view the way the app's View does and pins the language, so
// the assertions can name the copy they expect.
func frame(t *testing.T, w, h int, view func(c *ui.Context)) *ui.Tester {
	t.Helper()
	lang := i18n.Current()
	t.Cleanup(func() { i18n.Set(lang) })
	i18n.Set(i18n.EN)
	tt := ui.NewTester(func(c *ui.Context) {
		theme.Apply(c, theme.Current(c))
		ui.Column(c).Fill().Children(func() { view(c) })
	}, w, h)
	tt.Frame()
	return tt
}

// savePNG leaves the frame on disk for a human to look at; nothing asserts on
// the pixels.
func savePNG(t *testing.T, tt *ui.Tester, name string) {
	t.Helper()
	f, err := os.Create(name)
	if err != nil {
		t.Fatalf("create %s: %v", name, err)
	}
	defer f.Close()
	if err := png.Encode(f, tt.Image()); err != nil {
		t.Fatalf("encode %s: %v", name, err)
	}
	t.Logf("wrote %s", name)
}

func memoryFixture() MemoryProps {
	return MemoryProps{
		Memory: protocol.Memory{
			Global: []protocol.Note{
				{At: "2026-11-14T09:12:00Z", Text: "The bridge listens on 127.0.0.1:7788"},
			},
			Work: []protocol.Note{
				{At: "2026-11-15T18:04:00Z", Text: "Ship with go build ./... before tagging"},
			},
			Chat: []protocol.Note{
				{At: "2026-11-16T08:30:00Z", Text: "Prefers terse replies without headers"},
			},
		},
		Suggestions: []protocol.MemorySuggestion{
			{Text: "Deploys go out on Fridays", Scope: "work", Reason: "said twice this week"},
			{Text: "The owner reads Feishu before mail", Scope: "global", Reason: "repeated in two chats"},
		},
	}
}

func accessFixture() AccessProps {
	base := time.Date(2026, 11, 16, 9, 0, 0, 0, time.UTC).UnixMilli()
	audit := make([]protocol.AuditEvent, 0, 5)
	for i, event := range []struct {
		kind, tool, detail string
	}{
		{"access", "Bash", "git status"},
		{"query", "Read", "internal/app/app.go"},
		{"approve", "Bash", "go test ./..."},
		{"deny", "Bash", "rm -rf build"},
		{"report", "", "daily digest"},
	} {
		audit = append(audit, protocol.AuditEvent{
			At:     base + int64(i)*60_000,
			Kind:   event.kind,
			Tool:   event.tool,
			Detail: event.detail,
			Level:  protocol.AccessOwner,
		})
	}
	return AccessProps{
		View: &protocol.AccessView{
			Chats: []protocol.AccessRow{
				{ChatID: "oc_owner_11111111", Level: protocol.AccessOwner, ConversationID: 3, Title: "Feishu ops", ChatOwner: "ou_owner_123456", Members: 2},
				{ChatID: "oc_group_22222222", Level: protocol.AccessGroup, ConversationID: 5, Title: "Weekly sync", ChatOwner: "ou_lead_987654", Members: 12},
				{ChatID: "oc_closed_33333333", Level: protocol.AccessClosed, ConversationID: 9, Title: "Stranger chat"},
			},
			EnvChats:  []string{"oc_env_44444444"},
			OwnerChat: "oc_owner_11111111",
		},
		Audit: audit,
	}
}

// usageFixture spends a dollar every fifth day and a dime otherwise, so the
// totals are the same numbers the assertions name.
func usageFixture() UsageProps {
	start := time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC)
	days := make([]protocol.UsageDay, 0, usageDays)
	for i := 0; i < usageDays; i++ {
		cost := 0.10
		if i%5 == 0 {
			cost = 1.00
		}
		days = append(days, protocol.UsageDay{
			Date:   start.AddDate(0, 0, i).Format("2006-01-02"),
			Cost:   cost,
			Tokens: 1000 * (i%7 + 1),
		})
	}
	return UsageProps{Days: days}
}

func taskFixture() TaskProps {
	return TaskProps{
		Task: &protocol.TaskDetail{
			ID:             42,
			Kind:           "app.feature",
			ConversationID: 7,
			Background:     true,
			Status:         "running",
			Title:          "Draft the release notes",
			Input:          json.RawMessage(`{"spec":"Write the 0.2 release notes from the merged PRs"}`),
			Outcome:        json.RawMessage(`{"ok":true,"files":3}`),
		},
		Open: true,
	}
}

func TestMemoryPanelListsScopesAndCurates(t *testing.T) {
	curated, dropped := 0, -1
	p := memoryFixture()
	p.OnCurate = func() { curated++ }
	p.OnDrop = func(index int) { dropped = index }

	tt := frame(t, 460, 760, func(c *ui.Context) { MemoryPanel(c, p) })
	for _, want := range []string{
		"Memory",
		"The bridge listens on 127.0.0.1:7788",
		"Ship with go build ./... before tagging",
		"Prefers terse replies without headers",
		"GLOBAL · 1",
		"Deploys go out on Fridays",
		"Apply 2",
	} {
		if !tt.HasText(want) {
			t.Errorf("memory panel is missing %q; it painted %v", want, tt.Texts())
		}
	}

	if err := tt.Click("Curate 24h"); err != nil {
		t.Fatalf("click curate: %v", err)
	}
	if curated != 1 {
		t.Errorf("curate fired %d times, want 1", curated)
	}
	if err := tt.Click("Drop"); err != nil {
		t.Fatalf("click drop: %v", err)
	}
	if dropped != 0 {
		t.Errorf("drop reported index %d, want 0", dropped)
	}
	savePNG(t, frame(t, 460, 760, func(c *ui.Context) { MemoryPanel(c, p) }), "/tmp/pitta-ops-memory.png")
}

func TestAccessPanelShowsChatsOwnerAndAudit(t *testing.T) {
	reloaded := 0
	p := accessFixture()
	p.OnReload = func() { reloaded++ }

	tt := frame(t, 460, 760, func(c *ui.Context) { AccessPanel(c, p) })
	for _, want := range []string{
		"Access",
		"Feishu ops",
		"Weekly sync",
		"Stranger chat",
		"Owner",
		"Group",
		"Closed",
		"…11111111", // the owner chat, truncated to its last characters
		"…44444444", // the chat .env keeps shut
		envLockedNote,
		"RECENT DECISIONS",
		"Bash · rm -rf build",
	} {
		if !tt.HasText(want) {
			t.Errorf("access panel is missing %q; it painted %v", want, tt.Texts())
		}
	}

	if err := tt.Click("Refresh"); err != nil {
		t.Fatalf("click refresh: %v", err)
	}
	if reloaded != 1 {
		t.Errorf("reload fired %d times, want 1", reloaded)
	}
	savePNG(t, tt, "/tmp/pitta-ops-access.png")
}

func TestUsageCalendarTotals(t *testing.T) {
	tt := frame(t, 460, 700, func(c *ui.Context) { UsageCalendar(c, usageFixture()) })
	for _, want := range []string{
		"Usage",
		"COST",
		"$33.60",
		"TOKENS",
		"PEAK 07-20",
		"$1.00",
		"less",
		"more",
	} {
		if !tt.HasText(want) {
			t.Errorf("usage calendar is missing %q; it painted %v", want, tt.Texts())
		}
	}
	savePNG(t, tt, "/tmp/pitta-ops-usage.png")
}

func TestTaskDialogShowsTaskAndCloses(t *testing.T) {
	canceled, closed := 0, 0
	p := taskFixture()
	p.OnCancel = func() { canceled++ }
	p.OnClose = func() { closed++ }

	tt := frame(t, 720, 720, func(c *ui.Context) { TaskDialog(c, p) })
	for _, want := range []string{
		"Draft the release notes",
		"app.feature",
		"#42",
		"running",
		"Chat #7 · background",
		"Write the 0.2 release notes from the merged PRs",
		"LOGS",
		"Cancel",
		"Close",
	} {
		if !tt.HasText(want) {
			t.Errorf("task dialog is missing %q; it painted %v", want, tt.Texts())
		}
	}

	if err := tt.Click("Cancel"); err != nil {
		t.Fatalf("click cancel: %v", err)
	}
	if canceled != 1 {
		t.Errorf("cancel fired %d times, want 1", canceled)
	}
	if err := tt.Click("Close"); err != nil {
		t.Fatalf("click close: %v", err)
	}
	if closed != 1 {
		t.Errorf("close fired %d times, want 1", closed)
	}
	savePNG(t, frame(t, 720, 720, func(c *ui.Context) { TaskDialog(c, p) }), "/tmp/pitta-ops-task.png")
}

// TestPanelsPaint puts all four panels in one frame so a human can check the
// shadcn look at a glance.
func TestPanelsPaint(t *testing.T) {
	memory, access, usage, task := memoryFixture(), accessFixture(), usageFixture(), taskFixture()
	tt := frame(t, 1240, 820, func(c *ui.Context) {
		ui.Row(c).Fill().Children(func() {
			ui.Column(c).Width(400).FillHeight().Children(func() { MemoryPanel(c, memory) })
			ui.Column(c).Width(440).FillHeight().Children(func() { AccessPanel(c, access) })
			ui.Column(c).Width(400).FillHeight().Children(func() { UsageCalendar(c, usage) })
		})
		TaskDialog(c, task)
	})
	savePNG(t, tt, "/tmp/pitta-ops.png")
}
