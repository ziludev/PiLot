package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"pitta/internal/protocol"
)

// The bodies below are trimmed copies of real bridge replies, keeping fields
// this client does not know: decoding them must not fail.
const (
	stateBody = `{"feishu":{"ready":true,"since":1794817124474,"flaps":0,"started":true,"downMs":0},` +
		`"projects":[{"id":"workspace","name":"workspace","path":"/Users/x/workspace","builtin":true,"createdAt":1794282756232}],` +
		`"surfaces":[{"surfaceId":"cli","conversationId":2,"kind":"other","cost":0.1177,"tokens":774732,"title":"hello","archived":false,"projectId":"workspace"}],` +
		`"tasks":[{"id":1084,"kind":"app.usage-sampler","conversationId":1,"background":true,"state":"sleeping","until":1794819765170,"queue":[]}],` +
		`"usage":{"cost":0.1177,"tokens":774732}}`

	conversationBody = `{"conversationId":2,"hasMore":true,"approvalWaits":[],` +
		`"usage":{"cost":0.1177,"tokens":774732,"turns":34},` +
		`"model":{"provider":"magpie","modelId":"sitin-deepseek/deepseek-flash"},"thinkingLevel":"high",` +
		`"entries":[{"id":59,"kind":"pi.tool-result","model":[{"role":"toolResult","timestamp":1794819200000}],` +
		`"data":{"role":"toolResult"}}]}`

	tasksBody = `{"tasks":[{"id":1084,"kind":"app.usage-sampler","title":"采样","state":"sleeping",` +
		`"until":1794819765170,"conversationId":1,"owner":null,"retries":0}]}`

	approvalsBody = `{"approvals":[{"callId":"call_9","grantKey":"g1","tool":"bash","summary":"rm -rf build",` +
		`"reason":"needs the shell","surfaceId":"cli","createdAt":1794819200000}]}`

	healthBody = `{"pid":39217,"startedAt":"2026-11-18T02:05:11.412Z","uptimeMs":11520000,"version":"0.4.2",` +
		`"queueDepth":2,"runningTurns":1,"conversations":7,"somethingNew":true}`
)

type capture struct {
	method string
	path   string
	body   map[string]any
	auth   string
	raw    string
}

// serve answers every request with body and records what arrived.
func serve(t *testing.T, body string, got *capture) *Client {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.method, got.path, got.auth = r.Method, r.URL.RequestURI(), r.Header.Get("authorization")
		raw, _ := io.ReadAll(r.Body)
		got.raw = string(raw)
		if len(raw) > 0 {
			_ = json.Unmarshal(raw, &got.body)
		}
		w.Header().Set("content-type", "application/json")
		io.WriteString(w, body)
	}))
	t.Cleanup(server.Close)
	return New(server.URL, "s3cret")
}

func TestState(t *testing.T) {
	var got capture
	client := serve(t, stateBody, &got)
	state, err := client.State(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.method != http.MethodGet || got.path != "/api/state" {
		t.Fatalf("got %s %s", got.method, got.path)
	}
	if got.auth != "Bearer s3cret" {
		t.Fatalf("auth header %q", got.auth)
	}
	if len(state.Surfaces) != 1 || state.Surfaces[0].ConversationID != 2 {
		t.Fatalf("surfaces %+v", state.Surfaces)
	}
	if len(state.Projects) != 1 || state.Projects[0].ID != "workspace" {
		t.Fatalf("projects %+v", state.Projects)
	}
}

func TestTasks(t *testing.T) {
	var got capture
	client := serve(t, tasksBody, &got)
	tasks, err := client.Tasks(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.path != "/api/tasks" {
		t.Fatalf("path %s", got.path)
	}
	if len(tasks) != 1 || tasks[0].Title != "采样" || tasks[0].State != "sleeping" {
		t.Fatalf("tasks %+v", tasks)
	}
}

func TestConversationPage(t *testing.T) {
	var got capture
	client := serve(t, conversationBody, &got)
	conversation, err := client.Conversation(context.Background(), 2)
	if err != nil {
		t.Fatal(err)
	}
	if got.path != "/api/conversation?id=2&limit=120" {
		t.Fatalf("path %s", got.path)
	}
	if !conversation.HasMore || len(conversation.Entries) != 1 {
		t.Fatalf("conversation %+v", conversation)
	}
}

func TestConversationTail(t *testing.T) {
	var got capture
	client := serve(t, conversationBody, &got)
	if _, err := client.ConversationTail(context.Background(), 7); err != nil {
		t.Fatal(err)
	}
	if got.path != "/api/conversation?id=7&limit=1" {
		t.Fatalf("path %s", got.path)
	}
}

func TestSubmit(t *testing.T) {
	var got capture
	client := serve(t, `{}`, &got)
	err := client.Submit(context.Background(), SubmitRequest{
		ConversationID: 4,
		Text:           "hello",
		Mode:           ModeFollowUp,
		Images:         []Image{{Data: "aGk=", MimeType: "image/png"}},
		RequestID:      "req-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.method != http.MethodPost || got.path != "/api/submit" {
		t.Fatalf("got %s %s", got.method, got.path)
	}
	if got.body["conversationId"] != float64(4) || got.body["text"] != "hello" || got.body["mode"] != "followUp" {
		t.Fatalf("body %v", got.body)
	}
	images, _ := got.body["images"].([]any)
	if len(images) != 1 {
		t.Fatalf("images %v", got.body["images"])
	}
}

func TestAbort(t *testing.T) {
	var got capture
	client := serve(t, `{"cancelled":1}`, &got)
	outcome, err := client.Abort(context.Background(), 4, true)
	if err != nil {
		t.Fatal(err)
	}
	if got.path != "/api/abort" || got.body["conversationId"] != float64(4) || got.body["all"] != true {
		t.Fatalf("got %s %v", got.path, got.body)
	}
	if outcome.Cancelled != 1 {
		t.Fatalf("outcome %+v", outcome)
	}
}

func TestApprovalsAndDecision(t *testing.T) {
	var got capture
	client := serve(t, approvalsBody, &got)
	pending, err := client.Approvals(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.path != "/api/approvals" || len(pending) != 1 || pending[0].CallID != "call_9" {
		t.Fatalf("approvals %+v", pending)
	}

	got = capture{}
	client = serve(t, `{}`, &got)
	if err := client.DecideApproval(context.Background(), ApprovalDecision{CallID: "call_9", Approved: true}); err != nil {
		t.Fatal(err)
	}
	if got.method != http.MethodPost || got.path != "/api/approvals" {
		t.Fatalf("got %s %s", got.method, got.path)
	}
	// The decision is addressed by call id alone; the protocol has no chat on it.
	if got.body["callId"] != "call_9" || got.body["approved"] != true {
		t.Fatalf("body %v", got.body)
	}
	if _, ok := got.body["conversationId"]; ok {
		t.Fatalf("decision carried a conversation: %v", got.body)
	}
}

func TestNewConversation(t *testing.T) {
	var got capture
	client := serve(t, `{"conversationId":41,"surfaceId":"client"}`, &got)
	convID, surfaceID, err := client.NewConversation(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if got.path != "/api/conversations" {
		t.Fatalf("path %s", got.path)
	}
	if convID != 41 || surfaceID != "client" {
		t.Fatalf("got %d %q", convID, surfaceID)
	}
}

func TestHealth(t *testing.T) {
	var got capture
	client := serve(t, healthBody, &got)
	health, err := client.Health(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.path != "/api/health" || health.QueueDepth != 2 {
		t.Fatalf("health %+v", health)
	}
}

func TestStatusError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "no", http.StatusBadGateway)
	}))
	t.Cleanup(server.Close)
	_, err := New(server.URL, "").State(context.Background())
	var status *StatusError
	if !errors.As(err, &status) {
		t.Fatalf("err %v", err)
	}
	if status.Code != http.StatusBadGateway || !strings.Contains(status.Error(), "502") {
		t.Fatalf("status %+v", status)
	}
}

func TestMemoryReadsAllThreeScopes(t *testing.T) {
	var got capture
	client := serve(t, `{"global":[{"at":"2026-11-18","text":"Alex 说中文"}],`+
		`"work":[{"at":"2026-11-18","text":"代理 7890"}],"chat":[{"at":"2026-11-19","text":"折叠归用户"}]}`, &got)
	memory, err := client.Memory(context.Background(), 42)
	if err != nil {
		t.Fatal(err)
	}
	if got.path != "/api/memory?chat=42" {
		t.Fatalf("path %s", got.path)
	}
	if len(memory.Global) != 1 || memory.Global[0].Text != "Alex 说中文" {
		t.Fatalf("global %+v", memory.Global)
	}
	if len(memory.Work) != 1 || len(memory.Chat) != 1 {
		t.Fatalf("memory %+v", memory)
	}
}

func TestCurateMemorySendsHoursAndKeepsFailure(t *testing.T) {
	var got capture
	client := serve(t, `{"suggestions":[{"text":"分支红线","scope":"work","reason":"重复三次"}],`+
		`"scanned":12,"tokens":3400,"error":"model timed out"}`, &got)
	curation, err := client.CurateMemory(context.Background(), 48)
	if err != nil {
		t.Fatal(err)
	}
	if got.method != http.MethodPost || got.path != "/api/memory/curate" {
		t.Fatalf("got %s %s", got.method, got.path)
	}
	if hours, _ := got.body["hours"].(float64); hours != 48 {
		t.Fatalf("hours %v", got.body["hours"])
	}
	if len(curation.Suggestions) != 1 || curation.Suggestions[0].Scope != "work" {
		t.Fatalf("curation %+v", curation)
	}
	if curation.Error != "model timed out" || curation.Scanned != 12 || curation.Tokens != 3400 {
		t.Fatalf("curation %+v", curation)
	}
}

func TestApplyMemorySendsTheKeptLines(t *testing.T) {
	var got capture
	client := serve(t, `{"added":1,"skipped":1}`, &got)
	applied, err := client.ApplyMemory(context.Background(), []protocol.MemorySuggestion{
		{Text: "a", Scope: "global"}, {Text: "b", Scope: "work", Reason: "why"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.path != "/api/memory/apply" {
		t.Fatalf("path %s", got.path)
	}
	lines, _ := got.body["suggestions"].([]any)
	if len(lines) != 2 {
		t.Fatalf("body %+v", got.body)
	}
	first, _ := lines[0].(map[string]any)
	if first["text"] != "a" || first["scope"] != "global" {
		t.Fatalf("line %+v", first)
	}
	if applied.Added != 1 || applied.Skipped != 1 {
		t.Fatalf("applied %+v", applied)
	}
}

func TestChangesAsksForTheChatsWorktree(t *testing.T) {
	var got capture
	client := serve(t, `{"repo":true,"branch":"app/x","workspace":"/tmp/w",`+
		`"files":[{"path":"a.go","status":"modified","added":3,"removed":1}],`+
		`"diff":"diff --git a/a.go b/a.go\n+one\n"}`, &got)
	changes, err := client.Changes(context.Background(), 7)
	if err != nil {
		t.Fatal(err)
	}
	if got.path != "/api/changes?conversationId=7" {
		t.Fatalf("path %s", got.path)
	}
	if !changes.Repo || changes.Branch != "app/x" || len(changes.Files) != 1 {
		t.Fatalf("changes %+v", changes)
	}
	if changes.Files[0].Added != 3 || changes.Files[0].Removed != 1 {
		t.Fatalf("counts %+v", changes.Files[0])
	}
}

func TestGitReadsTheCommitsAndTheCompareURL(t *testing.T) {
	var got capture
	client := serve(t, `{"branch":"app/x","base":"main","compareUrl":"https://h/c",`+
		`"commits":[{"hash":"abc1234","short":"abc1234","subject":"app: one","files":2,"added":5,"removed":1}],`+
		`"remote":{"kind":"github","webUrl":"https://h"}}`, &got)
	git, err := client.Git(context.Background(), 7, 20)
	if err != nil {
		t.Fatal(err)
	}
	if got.path != "/api/git?conversationId=7&limit=20" {
		t.Fatalf("path %s", got.path)
	}
	if len(git.Commits) != 1 || git.Commits[0].Subject != "app: one" {
		t.Fatalf("commits %+v", git.Commits)
	}
	if git.CompareURL != "https://h/c" || git.Base != "main" {
		t.Fatalf("git %+v", git)
	}
}

func TestCommitDiffSendsTheHashEscaped(t *testing.T) {
	var got capture
	client := serve(t, `{"hash":"abc","diff":"@@ -1 +1 @@"}`, &got)
	diff, err := client.CommitDiff(context.Background(), 7, "abc")
	if err != nil {
		t.Fatal(err)
	}
	if got.path != "/api/git/diff?conversationId=7&hash=abc" {
		t.Fatalf("path %s", got.path)
	}
	if diff != "@@ -1 +1 @@" {
		t.Fatalf("diff %q", diff)
	}
}

func TestTaskReadsOneDetail(t *testing.T) {
	var got capture
	client := serve(t, `{"id":7,"kind":"agent.run","conversationId":2,"owner":"cli","background":true,`+
		`"abortRequested":false,"status":"running","phase":"thinking","until":1794819765170,`+
		`"title":"整理日志","input":{"text":"go"},"outcome":null,`+
		`"subAgents":[{"id":"sub_1","kind":"agent.spawn","title":"读源码","state":"running"}],`+
		`"startedAt":1794819200000,"somethingNew":true}`, &got)
	task, err := client.Task(context.Background(), 7)
	if err != nil {
		t.Fatal(err)
	}
	if got.method != http.MethodGet || got.path != "/api/task?id=7" {
		t.Fatalf("got %s %s", got.method, got.path)
	}
	if task.ID != 7 || task.Kind != "agent.run" || task.Status != "running" || task.Title != "整理日志" {
		t.Fatalf("task %+v", task)
	}
	if task.Until != 1794819765170 {
		t.Fatalf("until %d", task.Until)
	}
	if len(task.SubAgents) != 1 || task.SubAgents[0].Title != "读源码" || task.SubAgents[0].State != "running" {
		t.Fatalf("subAgents %+v", task.SubAgents)
	}
}

func TestCancelTaskPostsTheID(t *testing.T) {
	var got capture
	client := serve(t, `{}`, &got)
	if err := client.CancelTask(context.Background(), 7); err != nil {
		t.Fatal(err)
	}
	if got.method != http.MethodPost || got.path != "/api/task/cancel" {
		t.Fatalf("got %s %s", got.method, got.path)
	}
	if id, _ := got.body["id"].(float64); id != 7 || len(got.body) != 1 {
		t.Fatalf("body %v", got.body)
	}
}

func TestUsageCalendarReadsTheDays(t *testing.T) {
	var got capture
	client := serve(t, `{"days":[{"date":"2026-11-17","cost":0.0117,"tokens":4200},`+
		`{"date":"2026-11-18","cost":0.1177,"tokens":774732}]}`, &got)
	days, err := client.UsageCalendar(context.Background(), 30)
	if err != nil {
		t.Fatal(err)
	}
	if got.method != http.MethodGet || got.path != "/api/usage-calendar?days=30" {
		t.Fatalf("got %s %s", got.method, got.path)
	}
	if len(days) != 2 {
		t.Fatalf("days %+v", days)
	}
	if days[0].Date != "2026-11-17" || days[0].Cost != 0.0117 || days[0].Tokens != 4200 {
		t.Fatalf("day 0 %+v", days[0])
	}
	if days[1].Date != "2026-11-18" || days[1].Cost != 0.1177 || days[1].Tokens != 774732 {
		t.Fatalf("day 1 %+v", days[1])
	}
}

func TestUsageCalendarReportsANon200(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "no", http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)
	days, err := New(server.URL, "").UsageCalendar(context.Background(), 30)
	if err == nil {
		t.Fatalf("500 was read as a calendar: %+v", days)
	}
	var status *StatusError
	if !errors.As(err, &status) {
		t.Fatalf("err %v", err)
	}
	if status.Code != http.StatusInternalServerError || status.Path != "/api/usage-calendar?days=30" {
		t.Fatalf("status %+v", status)
	}
}
