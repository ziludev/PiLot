package ops

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"pitta/internal/protocol"
	"pitta/internal/theme"
)

// The right side of a stage row is what tells a forty-minute pipeline apart
// from a hung one, so it has to read at a glance: seconds while it is short,
// minutes once it is long, and never a counter that jumps every frame.
func TestSpanTimeReadsAtAGlance(t *testing.T) {
	for _, tc := range []struct {
		ms   int64
		want string
	}{
		{0, ""},
		{400, "1s"},
		{42_000, "42s"},
		{59_500, "59s"},
		{60_000, "1m"},
		{264_000, "4m"},
		{3_600_000, "1h"},
		{7_800_000, "2h10m"},
	} {
		if got := spanTime(tc.ms); got != tc.want {
			t.Errorf("spanTime(%d) = %q, want %q", tc.ms, got, tc.want)
		}
	}
}

// A live stage counts to now; a settled one stops at the moment it settled.
func TestStageSpanStopsWhenTheStageDoes(t *testing.T) {
	start := int64(1_000_000)
	now := start + 600_000

	running := protocol.TaskSubAgent{StartedAt: &start, Live: true, State: "live"}
	if got := stageSpan(now, running); got != "10m" {
		t.Errorf("live stage span = %q, want %q", got, "10m")
	}

	settled := start + 240_000
	done := protocol.TaskSubAgent{StartedAt: &start, EndedAt: &settled, State: "done"}
	if got := stageSpan(now+600_000, done); got != "4m" {
		t.Errorf("finished stage span = %q, want %q", got, "4m")
	}

	pending := protocol.TaskSubAgent{State: "pending"}
	if got := stageMeta(now, pending); got != "" {
		t.Errorf("a stage that has not started says %q, want silence", got)
	}
}

// TestFailedStageReadsRed pins the one state a step count cannot show. On a
// pipeline whose only two outcomes used to look alike, a failed stage and a
// finished one both sat there quiet; the dot now carries that difference.
func TestFailedStageReadsRed(t *testing.T) {
	p := taskFixture()
	p.Task.Status = "failed"
	p.Task.Outcome = nil
	p.Task.SubAgents = []protocol.TaskSubAgent{{
		ConversationID: 42,
		Role:           "实现",
		Steps:          37,
		Tool:           "go test",
		StartedAt:      ago(3 * time.Minute),
		EndedAt:        ago(time.Minute),
		State:          stateFailed,
	}}

	tokens := theme.Light()
	tt := ui.NewTester(func(c *ui.Context) {
		theme.Apply(c, tokens)
		TaskDialog(c, p)
	}, 720, 720)
	tt.Frame()

	if !tt.HasText("2m · 37 steps · go test") {
		t.Fatalf("failed stage row is missing its span; it painted %v", tt.Texts())
	}
	rect, ok := tt.Find("实现")
	if !ok {
		t.Fatal("failed stage row is not painted")
	}
	img := tt.Image()
	found := false
	for y := int(rect.Y) - 2; y <= int(rect.Y+rect.H)+2 && !found; y++ {
		for x := int(rect.X) - 14; x < int(rect.X) && x >= 0; x++ {
			pixel := img.RGBAAt(x, y)
			if sameInk(pixel.R, tokens.Destructive.R) &&
				sameInk(pixel.G, tokens.Destructive.G) &&
				sameInk(pixel.B, tokens.Destructive.B) {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("failed stage dot is not red: %v", tokens.Destructive)
	}
}

// TestTaskDialogInDark paints the same card on the dark tokens: the three stage
// states have to stay apart there too, where a red dot and an amber one sit on
// a much darker field.
func TestTaskDialogInDark(t *testing.T) {
	p := taskFixture()
	p.Task.SubAgents = []protocol.TaskSubAgent{
		{ConversationID: 1, Role: "写技术设计文档", Steps: 419, Tool: "bash",
			StartedAt: ago(10 * time.Minute), EndedAt: ago(6 * time.Minute), State: "done"},
		{ConversationID: 2, Role: "为已实现的代码写测试用例", Steps: 152, Tool: "go test",
			StartedAt: ago(6 * time.Minute), Live: true, State: "live"},
		{ConversationID: 3, Role: "评审改动", Steps: 12, StartedAt: ago(4 * time.Minute),
			EndedAt: ago(3 * time.Minute), State: stateFailed},
		{ConversationID: 4, Role: "补充文档", State: "pending"},
	}
	tt := ui.NewTester(func(c *ui.Context) {
		theme.Use(c, true)
		TaskDialog(c, p)
	}, 720, 780)
	tt.Frame()
	if !tt.HasText("#42") {
		t.Fatalf("dark dialog is not painted: %v", tt.Texts())
	}
	savePNG(t, tt, "/tmp/pitta-task-dark.png")
}

// sameInk compares one channel within the tolerance a blended dot needs.
func sameInk(a, b uint8) bool {
	diff := int(a) - int(b)
	if diff < 0 {
		diff = -diff
	}
	return diff <= 8
}
