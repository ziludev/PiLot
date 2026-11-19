package app

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"pitta/internal/i18n"
	"pitta/internal/protocol"
)

func TestRailTaskGroupsOrderAndCount(t *testing.T) {
	later := time.Now().Add(9 * time.Hour).UnixMilli()
	sooner := time.Now().Add(2 * time.Minute).UnixMilli()
	tasks := []protocol.Task{
		{ID: 1, Kind: "app.routine", State: "sleeping", Until: later},
		{ID: 2, Kind: "app.feature", State: "running"},
		{ID: 3, Kind: "app.reminder", State: "sleeping", Until: sooner},
		{ID: 4, Kind: "app.routine", State: "blocked"},
		{ID: 5, Kind: "app.reminder", State: "completing"},
	}
	groups := railTaskGroups(tasks)
	if len(groups) != 3 {
		t.Fatalf("got %d groups, want 3 (working, waiting, sleeping)", len(groups))
	}
	// Working first, then what is holding on the owner, then what is asleep: the
	// order a person reads them in.
	if groups[0].label != railTaskState("running") || len(groups[0].tasks) != 2 {
		t.Fatalf("working = %q with %d, want %q with 2", groups[0].label, len(groups[0].tasks), railTaskState("running"))
	}
	if groups[1].label != i18n.T("needsYou") || len(groups[1].tasks) != 1 {
		t.Fatalf("waiting = %q with %d", groups[1].label, len(groups[1].tasks))
	}
	// The next thing to happen is the next row.
	if got := groups[2].tasks; len(got) != 2 || got[0].ID != 3 || got[1].ID != 1 {
		t.Fatalf("sleeping order = %v, want ids 3 then 1", got)
	}
}

func TestRailTaskGroupsDropsEmptyHeadings(t *testing.T) {
	groups := railTaskGroups([]protocol.Task{{ID: 1, Kind: "app.digest", State: "sleeping"}})
	if len(groups) != 1 || groups[0].label != railTaskState("sleeping") {
		t.Fatalf("got %+v, want only the sleeping heading", groups)
	}
}

func TestNextFireLabel(t *testing.T) {
	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 14, 19, 0, 0, time.Local)
	if got, want := nextFireLabel(today.UnixMilli()), i18n.T("nextFire")+" 14:19"; got != want {
		t.Fatalf("today = %q, want %q", got, want)
	}
	// A time that is not today needs its date: "05:00" alone reads as this
	// morning, and a routine that wakes at five tomorrow is not that.
	tomorrow := today.AddDate(0, 0, 1)
	if got, want := nextFireLabel(tomorrow.UnixMilli()), tomorrow.Format("01-02 15:04"); got != want {
		t.Fatalf("tomorrow = %q, want %q", got, want)
	}
	if got := nextFireLabel(0); got != "" {
		t.Fatalf("no until = %q, want empty", got)
	}
}

// TestSnapshotTasksView draws the jobs view, which is the whole point of
// promoting tasks out of the strip: a routine that wakes in five minutes and
// one that already ran must not look the same.
func TestSnapshotTasksView(t *testing.T) {
	for _, dark := range []bool{false, true} {
		a := snapshotData(t)
		a.prefs = prefs{Theme: "light", SurfaceFilter: "tasks"}
		if dark {
			a.prefs.Theme = "dark"
		}
		now := time.Now()
		wake := time.Date(now.Year(), now.Month(), now.Day(), 23, 45, 0, 0, time.Local)
		a.tasks = []protocol.Task{
			{ID: 7, Title: "话题 cf1be3", Kind: "app.reminder", State: "running"},
			{ID: 9, Title: "自检任务", Kind: "app.routine", State: "sleeping", Until: wake.UnixMilli()},
			{ID: 8, Title: "住址与饮品偏好", Kind: "app.reminder", State: "sleeping", Until: now.Add(30 * time.Hour).UnixMilli()},
		}
		tt := ui.NewTester(a.View, 1180, 820)
		tt.SetDark(dark)
		tt.Frame()

		// The heading is the view's own, not the chats'.
		assertHasText(t, tt, i18n.T("tasks"))
		// Everything the strip could not say: when it wakes, and what kind it is.
		assertHasText(t, tt, nextFireLabel(wake.UnixMilli()))
		assertHasText(t, tt, railTaskState("sleeping"))
		assertHasText(t, tt, railTaskLabel("app.routine"))
		assertHasText(t, tt, railTaskLabel("app.reminder"))
		name := "/tmp/pitta-app-tasks-light.png"
		if dark {
			name = "/tmp/pitta-app-tasks-dark.png"
		}
		writePNG(t, name, tt.Image())
	}
}

// TestRailTaskMetaDropsTheKindItAlreadyNamed keeps a nameless job from reading
// "Routine" and then "#84338 · Routine": the kind is only worth a second line
// when the name is a real name.
func TestRailTaskMetaDropsTheKindItAlreadyNamed(t *testing.T) {
	if got := railTaskMeta(protocol.Task{ID: 84338, Kind: "app.routine"}); got != "#84338" {
		t.Fatalf("nameless meta = %q, want #84338", got)
	}
	got := railTaskMeta(protocol.Task{ID: 12, Kind: "app.routine", Title: "Daily review"})
	if got != "#12 · Routine" {
		t.Fatalf("named meta = %q, want #12 · Routine", got)
	}
}
