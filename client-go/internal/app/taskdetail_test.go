package app

import (
	"encoding/json"
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
	// Let the dialog's own read land and settle, then hand it the body the stub
	// was given: the stub answers the detail and the step list, and which of the
	// two arrives last is not this test's business.
	tt.Frame()
	tt.Frame()
	var task api.TaskDetail
	if err := json.Unmarshal([]byte(body), &task); err != nil {
		t.Fatalf("fixture body: %v", err)
	}
	a.applyTaskDetail(id, a.detail.seq, &task, nil)
	tt.Frame()

	for _, want := range []string{"Follow up on the review thread", "运行中", "输入", "步骤", "扫日志", "停止任务", "关闭"} {
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
	// The words, not the key: i18n.T("taskStopLeaves") would pass even if the
	// table had no such entry, because T falls back to the key it was given.
	if !tt.HasText("接着说一句就能续") {
		t.Fatalf("a stopped task did not say what survives; drew %v", tt.Texts())
	}
}

// detailWithLog puts a task and its step list into the dialog the way the frame
// loop does, so these tests can talk about what is drawn.
func detailWithLog(t *testing.T, a *App, tt *ui.Tester, task api.TaskDetail, stages []api.TaskLogStage) {
	t.Helper()
	a.openTaskDetail(task.ID)
	// Let the dialog's own reads land first: they answer the same question from
	// the stub bridge, and whichever arrives last is what the dialog shows. The
	// frame after this is the one under test.
	tt.Frame()
	tt.Frame()
	a.applyTaskDetail(task.ID, a.detail.seq, &task, nil)
	a.applyTaskLog(task.ID, a.detail.seq, stages, nil)
}

// TestAStageOpensOntoItsSteps: the child list used to say "跑了 · 2 步" and leave
// the rest to a chat the rail does not even list. A click has to turn that row
// into the calls it made — which command, and whether it went red.
func TestAStageOpensOntoItsSteps(t *testing.T) {
	i18n.Set(i18n.ZH)
	a := fixtureApp(t, false)
	tt := ui.NewTester(a.View, 1180, 820)
	detailWithLog(t, a, tt, api.TaskDetail{
		ID: 9, Kind: "pi.generation", ConversationID: 11, Status: "running", Title: "Fix the build",
		SubAgents: []api.TaskSubAgent{{ID: "s1", Kind: "pi.explore", Title: "扫日志", State: "running", ConversationID: 42}},
	}, []api.TaskLogStage{{
		ConversationID: 42,
		Steps: []api.TaskLogStep{
			{ID: 1, Kind: "tool", Tool: "bash", Label: "go test ./...", State: "failed"},
			{ID: 2, Kind: "say", Text: "The build is red in internal/app.", State: "ok"},
		},
	}})

	tt.Frame()
	if tt.HasText("go test ./...") {
		t.Fatal("the steps were open before anyone asked")
	}
	if !tt.HasText("扫日志") {
		t.Fatalf("the stage row is not drawn; drew %v", tt.Texts())
	}

	if err := tt.Click("扫日志"); err != nil {
		t.Fatalf("the stage row was not clickable: %v", err)
	}
	tt.Frame()
	writePNG(t, "/tmp/task-steps.png", tt.Image())
	for _, want := range []string{"bash", "go test ./...", "✗ 1"} {
		if !tt.HasText(want) {
			t.Errorf("the open stage does not show %q; drew %v", want, tt.Texts())
		}
	}

	// A second click closes it: one open stage at a time, and none when the user
	// is done reading.
	if err := tt.Click("扫日志"); err != nil {
		t.Fatalf("the open row was not clickable: %v", err)
	}
	tt.Frame()
	if tt.HasText("go test ./...") {
		t.Fatal("the steps stayed open after a second click")
	}
}

// TestATaskWithNoChildrenShowsItsOwnTurn: a pipeline is read through its stages;
// an ordinary turn is its own stage, and that is where its steps come from.
func TestATaskWithNoChildrenShowsItsOwnTurn(t *testing.T) {
	i18n.Set(i18n.ZH)
	a := fixtureApp(t, false)
	tt := ui.NewTester(a.View, 1180, 820)
	detailWithLog(t, a, tt, api.TaskDetail{
		ID: 9, Kind: "pi.generation", ConversationID: 11, Status: "terminal", Title: "Why is the sky blue",
	}, []api.TaskLogStage{{
		ConversationID: 11,
		Parent:         true,
		Steps:          []api.TaskLogStep{{ID: 1, Kind: "tool", Tool: "bash", Label: "go build ./...", State: "ok"}},
	}})

	tt.Frame()
	if !tt.HasText(i18n.T("taskDid")) {
		t.Fatalf("a task with no children did not offer its own steps; drew %v", tt.Texts())
	}
	if err := tt.Click(i18n.T("taskDid")); err != nil {
		t.Fatalf("its own stage was not clickable: %v", err)
	}
	tt.Frame()
	if !tt.HasText("go build ./...") {
		t.Fatalf("its own steps did not open; drew %v", tt.Texts())
	}
}

// TestTheStepListKeepsItsTail: a long stage is cut to what a dialog holds, and it
// is the end of the run that survives — that is where a run stops or breaks.
func TestTheStepListKeepsItsTail(t *testing.T) {
	i18n.Set(i18n.ZH)
	a := fixtureApp(t, false)
	steps := make([]api.TaskLogStep, 0, 30)
	for i := 0; i < 30; i++ {
		steps = append(steps, api.TaskLogStep{ID: i, Kind: "tool", Tool: "bash", Label: "step " + itoa(i), State: "ok"})
	}
	tt := ui.NewTester(a.View, 1180, 820)
	detailWithLog(t, a, tt, api.TaskDetail{
		ID: 9, Kind: "pi.generation", ConversationID: 11, Status: "terminal", Title: "Long one",
	}, []api.TaskLogStage{{ConversationID: 11, Parent: true, Steps: steps, More: 5}})

	tt.Frame()
	if err := tt.Click(i18n.T("taskDid")); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("step 29") {
		t.Fatalf("the tail was cut instead of the head; drew %v", tt.Texts())
	}
	if !tt.HasText("还有 15 步") {
		t.Fatalf("the rows left out were not counted; drew %v", tt.Texts())
	}
}

// TestTheRowThatStartedTheTaskIsNotABash: a pipeline's own rows begin after the
// decision to run it, so a step list built only from those starts at "bash" and
// never says the agent picked a skill. The call belongs to the turn above, and
// it is the row that explains the rest.
func TestTheRowThatStartedTheTaskIsNotABash(t *testing.T) {
	i18n.Set(i18n.ZH)
	a := fixtureApp(t, false)
	tt := ui.NewTester(a.View, 1180, 820)
	detailWithLog(t, a, tt, api.TaskDetail{
		ID: 9, Kind: "app.feature", ConversationID: 11, Status: "terminal", Title: "Add a step list",
		SubAgents: []api.TaskSubAgent{{ID: "s1", Kind: "pi.generation", Title: "写设计文档", State: "terminal", ConversationID: 42}},
	}, []api.TaskLogStage{
		{ConversationID: 11, Parent: true, Start: true, Steps: []api.TaskLogStep{
			{ID: 1, Kind: "tool", Tool: "delegate", Label: "写测试并把失败的补上", State: "ok"},
		}},
		{ConversationID: 42, Steps: []api.TaskLogStep{
			{ID: 2, Kind: "tool", Tool: "bash", Label: "go test ./...", State: "ok"},
		}},
	})

	tt.Frame()
	writePNG(t, "/tmp/task-start.png", tt.Image())
	if !tt.HasText(i18n.T("taskStartedFrom")) {
		t.Fatalf("the start row is missing; drew %v", tt.Texts())
	}
	// Open without a click: one row is an answer, not a place to explore.
	if !tt.HasText("delegate") || !tt.HasText("写测试并把失败的补上") {
		t.Fatalf("the start row does not say what started it; drew %v", tt.Texts())
	}
	// And the stages stay shut until asked, so the dialog is still a summary.
	if tt.HasText("go test ./...") {
		t.Fatal("a stage opened itself")
	}
}
