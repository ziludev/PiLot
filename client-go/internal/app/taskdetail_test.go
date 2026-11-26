package app

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"pilot/internal/api"
	"pilot/internal/i18n"
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
	// The bridge answers on its own goroutines; the test reads the log after.
	var calls []call
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		calls = append(calls, call{r.Method, r.URL.RequestURI(), string(body)})
		mu.Unlock()
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
	mu.Lock()
	logged := append([]call(nil), calls...)
	mu.Unlock()
	for _, c := range logged {
		if c.path == "/api/task/cancel" && c.method == http.MethodPost && c.body == `{"id":9}` {
			stopped = true
		}
	}
	if !stopped {
		t.Fatalf("stop never reached the bridge: %+v", logged)
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

// TestLongTaskInputIsAllThere: a task's input is JSON of whatever length its
// kind asked for. Clipped to a dozen lines, the dialog showed a text it could
// not finish and had nowhere to scroll — so the reader was told the input went
// on but not what it said.
func TestLongTaskInputIsAllThere(t *testing.T) {
	i18n.Set(i18n.ZH)
	a := fixtureApp(t, false)
	tt := ui.NewTester(a.View, 1180, 820)

	input := map[string]any{}
	for i := 0; i < 60; i++ {
		input["field"+itoa(i)] = i
	}
	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}

	detailWithLog(t, a, tt, api.TaskDetail{
		ID: 9, Kind: "pi.generation", ConversationID: 11, Status: "dormant",
		Title: "Recap the day", Input: raw,
	}, nil)
	tt.Frame()

	// The last field is the proof the whole block was laid out, not the first
	// twelve lines of it.
	if !tt.HasText(`"field59"`) {
		t.Fatalf("the input stopped early; drew %v", tt.Texts())
	}
	// And the body it sits in has somewhere to go: MaxY is what the wheel has to
	// travel, and a dialog whose overflow is zero cannot show its own last line.
	tt.Advance(400 * time.Millisecond)
	if a.detail.scroll.MaxY <= 0 {
		t.Fatal("the dialog has no room to scroll, so its tail is unreachable")
	}
}

// TestRescheduleReplacesTheRoutine: a routine's clock is the one setting the
// dialog can change. The bridge replaces the sleeping task rather than moving
// its deadline, so the dialog has to follow the id it gets back — staying on
// the old one would show a schedule nobody is keeping.
func TestRescheduleReplacesTheRoutine(t *testing.T) {
	i18n.Set(i18n.ZH)
	a := fixtureApp(t, false)
	tt := ui.NewTester(a.View, 1180, 820)

	input := json.RawMessage(`{"schedule":{"kind":"daily","at":540},"text":"看红灯","surfaceId":"feishu:1","anchor":1}`)
	detailWithLog(t, a, tt, api.TaskDetail{
		ID: 7, Kind: "app.routine", ConversationID: 11, Status: "sleeping",
		Until: time.Now().Add(6 * time.Hour).UnixMilli(), Title: "每天早上看红灯", Input: input,
	}, nil)
	tt.Frame()

	// The schedule is stated, not just editable: "when" is the question a
	// sleeping task is opened to answer.
	if !tt.HasText("daily 09:00") {
		t.Fatalf("the dialog does not say the schedule; drew %v", tt.Texts())
	}
	if err := tt.Click("改日程"); err != nil {
		t.Fatalf("the change button was not clickable: %v", err)
	}
	tt.Frame()
	if !a.detail.editing {
		t.Fatal("clicking the button did not open the picker")
	}
	// The picker opens on what is running: daily, 09:00 — not on an empty form
	// the reader has to type the old schedule into before changing one field.
	if a.detail.freq != 0 || a.detail.at.Hour() != 9 || a.detail.at.Minute() != 0 {
		t.Fatalf("the picker opened on freq=%d at=%s", a.detail.freq, a.detail.at.Format("15:04"))
	}

	posted := make(chan map[string]any, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/task/reschedule" {
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			posted <- body
			_, _ = io.WriteString(w, `{"ok":true,"id":12}`)
			return
		}
		_, _ = io.WriteString(w, "{}")
	}))
	t.Cleanup(server.Close)
	a.api = api.New(server.URL, "")

	a.detail.freq = 2
	a.detail.at = time.Date(a.detail.at.Year(), a.detail.at.Month(), a.detail.at.Day(), 8, 30, 0, 0, a.detail.at.Location())
	tt.Frame()
	if err := tt.Click("保存"); err != nil {
		t.Fatalf("the save button was not clickable: %v", err)
	}
	select {
	case body := <-posted:
		if body["schedule"] != "weekly mon 08:30" {
			t.Fatalf("posted %v", body)
		}
		if id, _ := body["id"].(float64); id != 7 {
			t.Fatalf("posted for task %v", body["id"])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the save never reached the bridge")
	}

	a.applyTaskReschedule(7, a.detail.seq, 12, nil)
	if !a.detail.open || a.detail.id != 12 {
		t.Fatalf("the dialog stayed on %d", a.detail.id)
	}
	if a.detail.editing {
		t.Fatal("the field stayed open after the swap")
	}
}

// TestRescheduleIsOnlyForASleepingRoutine: a round in flight is already
// deciding what it says, and a non-routine has no clock at all.
func TestRescheduleIsOnlyForASleepingRoutine(t *testing.T) {
	a := fixtureApp(t, false)
	cases := []struct {
		kind, status string
		want         bool
	}{
		{"app.routine", "sleeping", true},
		{"app.routine", "running", false},
		{"app.routine", "terminal", false},
		{"pi.generation", "sleeping", false},
	}
	for _, c := range cases {
		a.detail = taskDetailPanel{open: true, id: 1, task: &api.TaskDetail{Kind: c.kind, Status: c.status}}
		if got := a.canReschedule(); got != c.want {
			t.Errorf("%s/%s: canReschedule = %v", c.kind, c.status, got)
		}
	}
}

// TestReadScheduleLoadsThePicker: every shape a routine fires in lands in the
// picker as the choice that says the same thing.
func TestReadScheduleLoadsThePicker(t *testing.T) {
	now := time.Date(2026, 11, 23, 21, 0, 0, 0, time.Local)
	cases := []struct {
		input string
		freq  int
		clock string
		every float64
	}{
		{`{"schedule":{"kind":"daily","at":540}}`, 0, "09:00", 0},
		{`{"schedule":{"kind":"weekdays","at":570}}`, 1, "09:30", 0},
		{`{"schedule":{"kind":"weekly","weekday":1,"at":510}}`, 2, "08:30", 0},
		{`{"schedule":{"kind":"every","everyMs":21600000}}`, 3, "", 6},
		{`{"prompt":"nothing"}`, 0, "21:00", 0},
	}
	for _, c := range cases {
		fields := readSchedule(json.RawMessage(c.input), now)
		if fields.freq != c.freq {
			t.Errorf("%s: freq %d", c.input, fields.freq)
		}
		if c.every != 0 && fields.every != c.every {
			t.Errorf("%s: every %v", c.input, fields.every)
		}
		if c.clock != "" && fields.at.Format("15:04") != c.clock {
			t.Errorf("%s: at %s", c.input, fields.at.Format("15:04"))
		}
	}
}

// TestScheduleTextReadsBackWhatWasWritten: the field opens on the schedule
// that is running, in the same words the routine tool takes.
func TestScheduleTextReadsBackWhatWasWritten(t *testing.T) {
	cases := []struct{ input, want string }{
		{`{"schedule":{"kind":"daily","at":540}}`, "daily 09:00"},
		{`{"schedule":{"kind":"weekdays","at":570}}`, "weekdays 09:30"},
		{`{"schedule":{"kind":"weekly","weekday":1,"at":510}}`, "weekly mon 08:30"},
		{`{"schedule":{"kind":"every","everyMs":21600000}}`, "every 6h"},
		{`{"schedule":{"kind":"every","everyMs":1800000}}`, "every 30m"},
		{`{"schedule":{"kind":"every","everyMs":172800000}}`, "every 2d"},
		{`{"prompt":"no schedule here"}`, ""},
		{``, ""},
	}
	for _, c := range cases {
		if got := scheduleText(json.RawMessage(c.input)); got != c.want {
			t.Errorf("%s → %q, want %q", c.input, got, c.want)
		}
	}
}

// TestThePickerIsClickedNotTyped: changing a routine's clock is choosing from
// four shapes and a time, not remembering the routine tool's syntax.
func TestThePickerIsClickedNotTyped(t *testing.T) {
	i18n.Set(i18n.ZH)
	a := fixtureApp(t, false)
	tt := ui.NewTester(a.View, 1180, 820)

	input := json.RawMessage(`{"schedule":{"kind":"daily","at":540},"text":"看红灯","surfaceId":"feishu:1","anchor":1}`)
	detailWithLog(t, a, tt, api.TaskDetail{
		ID: 7, Kind: "app.routine", ConversationID: 11, Status: "sleeping",
		Until: time.Now().Add(6 * time.Hour).UnixMilli(), Title: "每天早上看红灯", Input: input,
	}, nil)
	tt.Frame()
	if err := tt.Click("改日程"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()

	if err := tt.Click("工作日"); err != nil {
		t.Fatalf("weekdays was not clickable: %v", err)
	}
	tt.Frame()
	if a.detail.freq != 1 || a.scheduleLine() != "weekdays 09:00" {
		t.Fatalf("weekdays gave freq=%d line=%q", a.detail.freq, a.scheduleLine())
	}

	if err := tt.Click("每隔"); err != nil {
		t.Fatalf("every was not clickable: %v", err)
	}
	tt.Frame()
	if a.detail.freq != 3 || a.scheduleLine() != "every 6h" {
		t.Fatalf("every gave freq=%d line=%q", a.detail.freq, a.scheduleLine())
	}
}
