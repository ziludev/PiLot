package app

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"pitta/internal/api"
	"pitta/internal/i18n"
)

// TestTaskRowOpensItsDetail: a task row that cannot be opened is a row the user
// can only guess about.
func TestTaskRowOpensItsDetail(t *testing.T) {
	i18n.Set(i18n.ZH)
	a := fixtureApp(t, false)
	a.rail = railTasks
	a.invalidate()
	tt := ui.NewTester(a.View, 1180, 820)
	tt.Frame()

	rows := a.railRowsCached()
	target := -1
	for index, row := range rows {
		if row.kind == railTask {
			target = index
			break
		}
	}
	if target < 0 {
		t.Fatalf("fixture has no task row: %+v", rows)
	}
	id := rows[target].task.ID

	body := `{"id":9,"kind":"pi.generation","conversationId":11,"status":"running","title":"Follow up on the review thread","input":{"prompt":"read the week"},"subAgents":[{"id":"s1","kind":"pi.explore","title":"扫日志","state":"running"}]}`
	// Two copies: openTaskDetail reads once on its own, and the test reads once
	// more to hand the result to the dialog without waiting on a frame loop.
	a.api = api.New(stubBridge(t, body, body), "")
	tt.Frame()

	// The row itself is the way in: the click has to reach the dialog.
	if err := tt.Click("Follow up on the review thread"); err != nil {
		t.Fatalf("the task row was not clickable: %v", err)
	}
	if !a.detail.open || a.detail.id != id {
		t.Fatalf("clicking the row opened %+v", a.detail)
	}

	a.openTaskDetail(id)
	task, err := a.api.Task(t.Context(), id)
	if err != nil {
		t.Fatalf("task: %v", err)
	}
	a.applyTaskDetail(id, 0, &task, nil)
	tt.Frame()

	for _, want := range []string{"Follow up on the review thread", "运行中", "输入", "子任务", "扫日志", "停止任务", "关闭"} {
		if !tt.HasText(want) {
			t.Errorf("the dialog does not say %q; drew %v", want, tt.Texts())
		}
	}
}

// TestTaskDetailRetiresAStaleReply: opening a second task while the first is
// still loading must not let the first answer land on the second.
func TestTaskDetailRetiresAStaleReply(t *testing.T) {
	i18n.Set(i18n.ZH)
	a := fixtureApp(t, false)
	a.openTaskDetail(9)
	first := a.detail.seq
	a.openTaskDetail(10)
	second := a.detail.seq

	stale := &api.TaskDetail{ID: 9, Title: "old"}
	a.applyTaskDetail(9, first, stale, nil)
	if a.detail.task != nil {
		t.Fatalf("a reply for task 9 landed on task 10: %q", a.detail.task.Title)
	}

	fresh := &api.TaskDetail{ID: 10, Title: "new"}
	a.applyTaskDetail(10, second, fresh, nil)
	if a.detail.task == nil || a.detail.task.Title != "new" {
		t.Fatalf("the current task's reply was dropped: %+v", a.detail.task)
	}
}

// TestTaskDetailShowsTheBridgesRefusal: a dialog that hides a failure is a
// dialog that lies about the task.
func TestTaskDetailShowsTheBridgesRefusal(t *testing.T) {
	i18n.Set(i18n.ZH)
	a := fixtureApp(t, false)
	tt := ui.NewTester(a.View, 1180, 820)
	a.openTaskDetail(9)
	a.applyTaskDetail(9, 0, nil, errors.New("no such task"))
	tt.Frame()
	if !tt.HasText("no such task") {
		t.Fatalf("the failure was swallowed; drew %v", tt.Texts())
	}
}

// TestStopTaskAsksTheBridge: the button is the only thing standing between a
// stuck task and a user who has to wait for it.
func TestStopTaskAsksTheBridge(t *testing.T) {
	i18n.Set(i18n.ZH)
	a := fixtureApp(t, false)
	type call struct{ method, path, body string }
	var calls []call
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		calls = append(calls, call{r.Method, r.URL.RequestURI(), string(body)})
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/task" {
			_, _ = w.Write([]byte(`{"id":9,"status":"running","title":"Follow up on the review thread"}`))
			return
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()
	a.api = api.New(server.URL, "")
	a.openTaskDetail(9)
	a.applyTaskDetail(9, a.detail.seq, &api.TaskDetail{ID: 9, Status: "running", Title: "Follow up"}, nil)
	if !a.canStop() {
		t.Fatal("a running task offered no stop")
	}
	a.stopTask(9)
	time.Sleep(100 * time.Millisecond)
	a.applyTaskStop(9, a.detail.seq, nil)

	var stopped bool
	for _, c := range calls {
		if c.path == "/api/task/cancel" && c.method == http.MethodPost && c.body == `{"id":9}` {
			stopped = true
		}
	}
	if !stopped {
		t.Fatalf("stop never reached the bridge: %+v", calls)
	}
	if !a.detail.task.AbortRequested {
		t.Fatal("the dialog still reads as if the task were running")
	}
	if a.canStop() {
		t.Fatal("the stop button is still offered after a stop")
	}
}

// TestUsageGridReadsTheDays: the calendar's whole point is that a heavy day
// looks different from a quiet one, so the peak has to come from the data.
func TestUsageGridReadsTheDays(t *testing.T) {
	i18n.Set(i18n.ZH)
	a := fixtureApp(t, false)
	tt := ui.NewTester(a.View, 1180, 820)
	today := startOfDay(a.now)
	days := []api.UsageDay{
		{Date: today.AddDate(0, 0, -2).Format("2006-01-02"), Cost: 0.5, Tokens: 120000},
		{Date: today.AddDate(0, 0, -1).Format("2006-01-02"), Cost: 3.5, Tokens: 4000000},
		{Date: today.Format("2006-01-02"), Cost: 1.0, Tokens: 900000},
	}
	a.usage.open = true
	a.applyUsage(0, days, nil)
	tt.Frame()

	if !tt.HasText("用量日历") {
		t.Fatalf("the calendar did not draw; %v", tt.Texts())
	}
	// $5.00 across the three days, 5.02M tokens, and the heaviest day is the
	// one at 3.5.
	for _, want := range []string{"$5.00", "5M", "最贵的一天"} {
		if !tt.HasText(want) {
			t.Errorf("the summary does not say %q; drew %v", want, tt.Texts())
		}
	}
}

// TestUsagePanelDropsAClosedReply: the calendar can be shut while its numbers
// are still coming, and an answer for a closed panel is not used.
func TestUsagePanelDropsAClosedReply(t *testing.T) {
	a := fixtureApp(t, false)
	a.toggleUsage()
	open := a.usage.seq
	a.toggleUsage()
	if a.usage.open {
		t.Fatal("the second toggle did not close the calendar")
	}
	a.applyUsage(open, []api.UsageDay{{Date: "2026-11-01", Cost: 9}}, nil)
	if a.usage.days != nil {
		t.Fatalf("a closed calendar kept %+v", a.usage.days)
	}
}

// TestAStoppedTaskSaysWhatSurvives: linger's rule, and the right one — cancel is
// not close. A dialog that only says "stopped" leaves the user wondering whether
// the work is gone, and the answer (the chat and the files are still there) is
// the one thing that decides whether they dare stop anything again.
func TestAStoppedTaskSaysWhatSurvives(t *testing.T) {
	i18n.Set(i18n.ZH)
	a := fixtureApp(t, false)
	a.openTaskDetail(9)
	a.applyTaskDetail(9, a.detail.seq, &api.TaskDetail{ID: 9, Status: "aborted", Title: "Review the diff"}, nil)

	tt := ui.NewTester(a.View, 1180, 820)
	tt.Frame()
	if !tt.HasText(i18n.T("taskStopLeaves")) {
		t.Fatalf("a stopped task did not say what survives; drew %v", tt.Texts())
	}
}
