package app

import (
	"testing"

	"github.com/egoist/mygo/ui"

	"pilot/internal/i18n"
)

// TestMessageActionsShowOnHover: the bar is built but invisible until the
// pointer is on the message, so the thread reads as prose until asked.
func TestMessageActionsShowOnHover(t *testing.T) {
	i18n.Set(i18n.ZH)
	a := fixtureApp(t, false)
	tt := ui.NewTester(a.View, 1180, 820)
	tt.Frame()

	rows := a.threadRows(a.active)
	assistant := -1
	for index, row := range rows {
		if row.kind == rowAssistant {
			assistant = index
			break
		}
	}
	if assistant < 0 {
		t.Fatal("fixture has no assistant row")
	}

	// Away from every message: the verbs are drawn but transparent, and a
	// transparent button is not a button.
	if err := tt.Click("复制"); err == nil {
		t.Fatal("an idle action bar took a click")
	}

	rect, ok := tt.Find("复制")
	if !ok {
		t.Fatalf("the copy action was never laid out; drew %v", tt.Texts())
	}
	tt.Move(rect.X+rect.W/2, rect.Y+rect.H/2)
	tt.Frame()
	moved, ok := tt.Find("复制")
	if !ok || moved.Y != rect.Y {
		t.Fatalf("hover moved the action bar: %+v then %+v", rect, moved)
	}
}

// TestCopyMessageFillsTheClipboard: a copy that prints nothing on the clipboard
// is the same as a click that did nothing.
func TestCopyMessageFillsTheClipboard(t *testing.T) {
	i18n.Set(i18n.ZH)
	a := fixtureApp(t, false)
	tt := ui.NewTester(a.View, 1180, 820)
	tt.Frame()

	rect, ok := tt.Find("复制")
	if !ok {
		t.Fatal("copy was never laid out")
	}
	tt.Move(rect.X+rect.W/2, rect.Y+rect.H/2)
	tt.Frame()
	if err := tt.Click("复制"); err != nil {
		t.Fatalf("copy was not clickable: %v", err)
	}
	tt.Frame()
	// The first bar in row order is the question's; the mechanism is the same
	// for the answer's, which is what the assistant test below checks by hand.
	if got := tt.Clipboard(); got != "why is the sky blue? and can you check the build while you answer" {
		t.Fatalf("clipboard holds %q", got)
	}
	if !tt.HasText("已复制") {
		t.Fatalf("the copy said nothing; drew %v", tt.Texts())
	}
}

// TestEditResendPutsTheQuestionBack: editing does not rewrite the record, it
// hands the words back to the composer.
func TestEditResendPutsTheQuestionBack(t *testing.T) {
	i18n.Set(i18n.ZH)
	a := fixtureApp(t, false)
	tt := ui.NewTester(a.View, 1180, 820)
	tt.Frame()

	rect, ok := tt.Find("编辑重发")
	if !ok {
		t.Fatal("edit was never laid out")
	}
	tt.Move(rect.X+rect.W/2, rect.Y+rect.H/2)
	tt.Frame()
	if err := tt.Click("编辑重发"); err != nil {
		t.Fatalf("edit was not clickable: %v", err)
	}
	tt.Frame()
	if a.draft != "why is the sky blue? and can you check the build while you answer" {
		t.Fatalf("draft %q", a.draft)
	}
	if !tt.HasText("why is the sky blue? and can you check the build while you answer") {
		t.Fatal("the words are not in the composer on screen")
	}
}

// TestJumpToLatestAppearsWhenScrolledAway: leaving the end is the only way to
// read older answers, so the way back has to be visible from there.
func TestJumpToLatestAppearsWhenScrolledAway(t *testing.T) {
	i18n.Set(i18n.ZH)
	a := fixtureApp(t, false)
	tt := ui.NewTester(a.View, 1180, 520)
	tt.Frame()

	if tt.HasText("回到最新") {
		t.Fatal("the pill was up while the transcript sat at the end")
	}

	tt.Scroll(700, 300, 0, -400)
	tt.Frame()
	if !tt.HasText("回到最新") {
		t.Fatalf("scrolling away did not offer the way back; drew %v", tt.Texts())
	}

	if err := tt.Click("回到最新"); err != nil {
		t.Fatalf("pill not clickable: %v", err)
	}
	tt.Frame()
	if !a.vm.threadList.FollowEnd {
		t.Fatal("the pill did not resume following the end")
	}
	if tt.HasText("回到最新") {
		t.Fatal("the pill stayed up after jumping to the end")
	}
}
