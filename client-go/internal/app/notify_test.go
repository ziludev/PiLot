package app

import (
	"testing"

	"pitta/internal/i18n"
	"pitta/internal/protocol"
)

// A snapshot that has a turn running in chat 12, named by its surface.
func busy(conv int, title string) ([]protocol.Surface, []protocol.Task) {
	return []protocol.Surface{{SurfaceID: "s-" + title, ConversationID: conv, Title: title}},
		[]protocol.Task{{ConversationID: conv, State: "running"}}
}

func idle(conv int, title, stop string) []protocol.Surface {
	return []protocol.Surface{{SurfaceID: "s-" + title, ConversationID: conv, Title: title, LastStop: stop}}
}

func nobody(int) bool { return false }

func TestNotifierPrimesOnTheFirstSnapshot(t *testing.T) {
	i18n.Set(i18n.ZH)
	var n notifier
	surfaces, tasks := busy(12, "修表格")
	pending := []protocol.PendingApproval{{CallID: "c1", SurfaceID: "s-修表格", Tool: "bash"}}

	// Everything in the first snapshot happened before the window looked, so
	// none of it is news.
	if got := n.observe(surfaces, tasks, pending, nobody); len(got) != 0 {
		t.Fatalf("first snapshot rang %d times: %#v", len(got), got)
	}

	// The same turn ending afterwards is news.
	got := n.observe(idle(12, "修表格", ""), nil, pending, nobody)
	if len(got) != 1 || got[0].convID != 12 {
		t.Fatalf("want one banner for chat 12, got %#v", got)
	}
	if got[0].body != "跑完了" {
		t.Fatalf("body = %q", got[0].body)
	}
	if got[0].title != "修表格" {
		t.Fatalf("title = %q", got[0].title)
	}
}

func TestNotifierRingsOncePerFinishedTurn(t *testing.T) {
	i18n.Set(i18n.ZH)
	var n notifier
	surfaces, tasks := busy(12, "修表格")
	n.observe(surfaces, tasks, nil, nobody)

	if got := n.observe(idle(12, "修表格", ""), nil, nil, nobody); len(got) != 1 {
		t.Fatalf("first stop rang %d times", len(got))
	}
	// Polling keeps saying the same thing; the banner must not.
	for i := 0; i < 3; i++ {
		if got := n.observe(idle(12, "修表格", ""), nil, nil, nobody); len(got) != 0 {
			t.Fatalf("poll %d rang again: %#v", i, got)
		}
	}
	// A new turn that ends rings again.
	n.observe(surfaces, tasks, nil, nobody)
	if got := n.observe(idle(12, "修表格", ""), nil, nil, nobody); len(got) != 1 {
		t.Fatalf("second turn rang %d times", len(got))
	}
}

func TestNotifierStaysQuietForTheChatInFrontOfYou(t *testing.T) {
	i18n.Set(i18n.ZH)
	var n notifier
	surfaces, tasks := busy(12, "修表格")
	n.observe(surfaces, tasks, nil, nobody)

	watching := func(conv int) bool { return conv == 12 }
	if got := n.observe(idle(12, "修表格", ""), nil, nil, watching); len(got) != 0 {
		t.Fatalf("rang for the chat on screen: %#v", got)
	}
}

func TestNotifierSaysHowTheTurnEnded(t *testing.T) {
	for stop, want := range map[string]string{"error": "跑完了，但出错了", "abort": "已停下"} {
		i18n.Set(i18n.ZH)
		var n notifier
		surfaces, tasks := busy(12, "修表格")
		n.observe(surfaces, tasks, nil, nobody)
		got := n.observe(idle(12, "修表格", stop), nil, nil, nobody)
		if len(got) != 1 || got[0].body != want {
			t.Fatalf("stop %q: got %#v, want body %q", stop, got, want)
		}
	}
}

func TestNotifierNamesAChatTheBridgeHasNotNamed(t *testing.T) {
	i18n.Set(i18n.ZH)
	var n notifier
	n.observe([]protocol.Surface{{SurfaceID: "s", ConversationID: 42}},
		[]protocol.Task{{ConversationID: 42, State: "running"}}, nil, nobody)
	got := n.observe([]protocol.Surface{{SurfaceID: "s", ConversationID: 42}}, nil, nil, nobody)
	if len(got) != 1 || got[0].title != "会话 42" {
		t.Fatalf("got %#v", got)
	}
}

func TestNotifierRingsForANewApprovalOnce(t *testing.T) {
	i18n.Set(i18n.ZH)
	var n notifier
	surfaces := []protocol.Surface{{SurfaceID: "s-12", ConversationID: 12, Title: "修表格"}}
	n.observe(surfaces, nil, nil, nobody)

	pending := []protocol.PendingApproval{
		{CallID: "c1", SurfaceID: "s-12", Tool: "bash", Summary: "rm -rf build"},
	}
	got := n.observe(surfaces, nil, pending, nobody)
	if len(got) != 1 || got[0].convID != 12 || got[0].id != "ask-c1" {
		t.Fatalf("got %#v", got)
	}
	if got[0].body != "要你确认：bash · rm -rf build" {
		t.Fatalf("body = %q", got[0].body)
	}
	if again := n.observe(surfaces, nil, pending, nobody); len(again) != 0 {
		t.Fatalf("asked twice: %#v", again)
	}

	// Answered, then asked again: that is a new question.
	n.observe(surfaces, nil, nil, nobody)
	if again := n.observe(surfaces, nil, pending, nobody); len(again) != 1 {
		t.Fatalf("re-asked but stayed quiet: %#v", again)
	}
}

func TestNotifierSkipsTheApprovalOfTheChatOnScreen(t *testing.T) {
	i18n.Set(i18n.ZH)
	var n notifier
	surfaces := []protocol.Surface{{SurfaceID: "s-12", ConversationID: 12, Title: "修表格"}}
	n.observe(surfaces, nil, nil, nobody)

	pending := []protocol.PendingApproval{{CallID: "c1", SurfaceID: "s-12", Tool: "bash"}}
	got := n.observe(surfaces, nil, pending, func(conv int) bool { return conv == 12 })
	if len(got) != 0 {
		t.Fatalf("rang for the chat on screen: %#v", got)
	}
	// It was still recorded as seen, so it does not ring later out of nowhere.
	if again := n.observe(surfaces, nil, pending, nobody); len(again) != 0 {
		t.Fatalf("rang after the user looked away: %#v", again)
	}
}

func TestApprovalWhatFallsBackWhenTheBridgeSaysNothing(t *testing.T) {
	i18n.Set(i18n.ZH)
	if got := approvalWhat(protocol.PendingApproval{}); got != "一个写操作" {
		t.Fatalf("empty approval = %q", got)
	}
	if got := approvalWhat(protocol.PendingApproval{Tool: "bash"}); got != "bash" {
		t.Fatalf("tool only = %q", got)
	}
}

// TestSnapshotRingsForAFinishedTurn: the notifier is only as good as the
// snapshot it is handed, and this is the wiring — a turn that stops in a chat
// the window is not showing ends up as one banner.
func TestSnapshotRingsForAFinishedTurn(t *testing.T) {
	i18n.Set(i18n.ZH)
	a := fixtureApp(t, false)
	var got []alert
	a.banner = func(al alert) { got = append(got, al) }
	state := &protocol.State{Surfaces: []protocol.Surface{
		{SurfaceID: "oc_12", ConversationID: 12, Title: "Deploy checklist"},
		{SurfaceID: "client:9", ConversationID: 9, Title: "Sandbox scratch"},
	}}

	a.applySnapshot(&snapshotResult{ok: true, state: state, tasksOK: true,
		tasks: []protocol.Task{{ConversationID: 12, State: "running"}}})
	if len(got) != 0 {
		t.Fatalf("the first snapshot rang: %#v", got)
	}

	a.applySnapshot(&snapshotResult{ok: true, state: state, tasksOK: true})
	if len(got) != 1 || got[0].convID != 12 {
		t.Fatalf("banners = %#v", got)
	}
	if got[0].title != "Deploy checklist" || got[0].body != "跑完了" {
		t.Fatalf("banner = %#v", got[0])
	}
}
