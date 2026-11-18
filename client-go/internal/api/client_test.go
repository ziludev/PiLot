package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"pitta/internal/protocol"
)

// The bodies below are trimmed copies of real bridge replies (curl 127.0.0.1:8787),
// keeping the fields this client does not know: decoding them must not fail.
const (
	stateBody = `{"feishu":{"ready":true,"since":1794817124474,"flaps":0,"started":true,"downMs":0},` +
		`"projects":[{"id":"workspace","name":"workspace","path":"/Users/alexwang/learn/pi-durable-agent/workspace","builtin":true,"createdAt":1794282756232}],` +
		`"surfaces":[{"surfaceId":"cli","conversationId":2,"kind":"other","cost":0.1177,"tokens":774732,"title":"hello","archived":false,"projectId":"workspace"}],` +
		`"tasks":[{"id":1084,"kind":"app.usage-sampler","conversationId":1,"background":true,"state":"sleeping","until":1794819765170,"queue":[]}],` +
		`"usage":{"cost":0.1177,"tokens":774732}}`

	conversationBody = `{"conversationId":2,"approvalWaits":[{"start":1794285590789,"end":1794285603337,"tool":"bash"}],` +
		`"usage":{"cost":0.117704958,"tokens":774732,"turns":34},` +
		`"model":{"provider":"magpie","modelId":"sitin-deepseek/deepseek-flash"},"thinkingLevel":"high",` +
		`"entries":[{"id":59,"kind":"pi.tool-result","model":[{"role":"toolResult","toolCallId":"call_00","toolName":"bash"}],` +
		`"data":{"role":"toolResult","content":[{"type":"text","text":"ok"}],"isError":false,"durationMs":12044},"rev":42}],"rev":42}`

	memoryBody = `{"global":[{"at":"2026-11-08T02:10:00.000Z","text":"prefers conclusions first"}],` +
		`"work":[{"at":"2026-11-08T02:10:00.000Z","text":"works on pitta"}],` +
		`"chat":[{"at":"2026-11-09T03:18:55.580Z","text":"codename blue whale"}],"version":3}`

	changesBody = `{"repo":true,"diff":"","files":[{"path":"main.go","status":"M","added":3,"removed":1}],` +
		`"truncated":false,"workspace":"/Users/alexwang/learn/pi-durable-agent/workspace/.worktrees/conv-2",` +
		`"branch":"agent/conv-2","ahead":1}`

	gitBody = `{"worktree":"/Users/alexwang/learn/pi-durable-agent/workspace/.worktrees/conv-2","branch":"agent/conv-2","base":"main","origin":"main",` +
		`"commits":[{"hash":"9f2c1ab","short":"9f2c1ab","date":"2026-11-09T03:18:55.580Z","subject":"checkpoint","files":2,"added":10,"removed":4}],` +
		`"branches":[{"name":"agent/conv-2","current":true},{"name":"main","current":false,"upstream":"origin/main","track":"ahead"}],` +
		`"remote":{"url":"git@github.com:alex/pitta.git","kind":"github","webUrl":"https://github.com/alex/pitta"},` +
		`"compareUrl":"https://github.com/alex/pitta/compare/main...agent/conv-2","unpushed":1}`

	modelsBody = `{"models":[{"provider":"magpie","providerName":"Magpie","id":"sitin-deepseek/deepseek-flash","name":"DeepSeek Flash",` +
		`"reasoning":true,"contextWindow":128000,"maxTokens":8192,"cost":{"input":0.14,"output":0.28},"thinkingLevels":["off","high"],"releasedAt":"2026-01-01"}],` +
		`"default":{"provider":"magpie","modelId":"sitin-deepseek/deepseek-flash"}}`

	usageBody = `{"days":[{"date":"2026-11-09","cost":0.6236339219999998,"tokens":4101630},` +
		`{"date":"2026-11-10","cost":6.138926921999999,"tokens":40809588}],"window":120}`

	accessBody = `{"chats":[{"chatId":"oc_15c461fd7d8d9d92e3ef14ed58b12ee9","level":"group"},` +
		`{"chatId":"oc_2c45f662c3535d6a183199fb9574b3a9","level":"owner","conversationId":67,"title":"morning","chatOwner":"ou_af","members":3}],` +
		`"envChats":["oc_2c45f662c3535d6a183199fb9574b3a9","oc_cdb3bab990f517e8c6b5fe4998a1a13f"],` +
		`"ownerChat":"oc_2c45f662c3535d6a183199fb9574b3a9","source":"env"}`

	auditBody = `{"events":[{"kind":"deny","tool":"bash","detail":"guest surface","at":1794580724508},` +
		`{"kind":"access","who":"ou_af","chat":"oc_2c","level":"owner","at":1794580724513}],"next":"cursor"}`

	taskBody = `{"id":1084,"kind":"app.usage-sampler","conversationId":1,"owner":null,"background":true,` +
		`"abortRequested":false,"status":"sleeping","phase":null,"until":1794819765170,"input":null,"outcome":null,"retries":0}`

	healthBody = `{"pid":39217,"startedAt":"2026-11-18T02:05:11.412Z","uptimeMs":11520000,"version":"0.4.2",` +
		`"queueDepth":2,"runningTurns":1,"conversations":7,"somethingNew":true}`
)

func TestClientGET(t *testing.T) {
	const token = "s3cret"
	ctx := context.Background()

	tests := []struct {
		name  string
		path  string
		body  string
		call  func(*Client) (any, error)
		check func(*testing.T, any)
	}{
		{
			name: "state", path: "/api/state", body: stateBody,
			call: func(c *Client) (any, error) { return c.State(ctx) },
			check: func(t *testing.T, got any) {
				state := got.(*protocol.State)
				if len(state.Surfaces) != 1 || state.Surfaces[0].ConversationID != 2 || state.Surfaces[0].Title != "hello" {
					t.Fatalf("surfaces = %+v", state.Surfaces)
				}
				if len(state.Projects) != 1 || !state.Projects[0].Builtin {
					t.Fatalf("projects = %+v", state.Projects)
				}
				if len(state.Tasks) != 1 || state.Tasks[0].State != "sleeping" {
					t.Fatalf("tasks = %+v", state.Tasks)
				}
				if state.Feishu == nil || !state.Feishu.Ready || state.Feishu.DownMs != 0 {
					t.Fatalf("feishu = %+v", state.Feishu)
				}
			},
		},
		{
			name: "health", path: "/api/health", body: healthBody,
			call: func(c *Client) (any, error) { return c.Health(ctx) },
			check: func(t *testing.T, got any) {
				health := got.(*protocol.Health)
				if health.PID != 39217 || health.Version != "0.4.2" {
					t.Fatalf("health = %+v", health)
				}
				if health.QueueDepth != 2 || health.Running != 1 || health.Chats != 7 {
					t.Fatalf("counts = %+v", health)
				}
				if health.UptimeMS != 11520000 || health.StartedAt != "2026-11-18T02:05:11.412Z" {
					t.Fatalf("uptime = %+v", health)
				}
			},
		},
		{
			name: "conversation", path: "/api/conversation?id=2&limit=200", body: conversationBody,
			call: func(c *Client) (any, error) { return c.Conversation(ctx, 2) },
			check: func(t *testing.T, got any) {
				conversation := got.(*protocol.Conversation)
				if len(conversation.Entries) != 1 || conversation.Entries[0].ID != 59 {
					t.Fatalf("entries = %+v", conversation.Entries)
				}
				if !strings.Contains(string(conversation.Entries[0].Data), `"durationMs"`) {
					t.Fatalf("entry data = %s", conversation.Entries[0].Data)
				}
				if conversation.Model == nil || conversation.Model.Provider != "magpie" {
					t.Fatalf("model = %+v", conversation.Model)
				}
				if conversation.ThinkingLevel != "high" || conversation.Usage == nil || conversation.Usage.Turns != 34 {
					t.Fatalf("thinking = %q usage = %+v", conversation.ThinkingLevel, conversation.Usage)
				}
				if len(conversation.ApprovalWaits) != 1 || conversation.ApprovalWaits[0].Tool != "bash" {
					t.Fatalf("waits = %+v", conversation.ApprovalWaits)
				}
			},
		},
		{
			name: "memory", path: "/api/memory?chat=2", body: memoryBody,
			call: func(c *Client) (any, error) { return c.Memory(ctx, 2) },
			check: func(t *testing.T, got any) {
				memory := got.(*protocol.Memory)
				if len(memory.Global) != 1 || len(memory.Work) != 1 || len(memory.Chat) != 1 {
					t.Fatalf("memory = %+v", memory)
				}
				if memory.Chat[0].Text != "codename blue whale" {
					t.Fatalf("chat = %+v", memory.Chat)
				}
			},
		},
		{
			name: "changes", path: "/api/changes?conversationId=2", body: changesBody,
			call: func(c *Client) (any, error) { return c.Changes(ctx, 2) },
			check: func(t *testing.T, got any) {
				changes := got.(*protocol.Changes)
				if !changes.Repo || changes.Branch != "agent/conv-2" {
					t.Fatalf("changes = %+v", changes)
				}
				if len(changes.Files) != 1 || changes.Files[0].Added != 3 || changes.Files[0].Status != "M" {
					t.Fatalf("files = %+v", changes.Files)
				}
			},
		},
		{
			name: "git", path: "/api/git?conversationId=2", body: gitBody,
			call: func(c *Client) (any, error) { return c.Git(ctx, 2) },
			check: func(t *testing.T, got any) {
				git := got.(*protocol.Git)
				if git.Branch != "agent/conv-2" || git.Base != "main" || git.Worktree == "" {
					t.Fatalf("git = %+v", git)
				}
				if len(git.Commits) != 1 || git.Commits[0].Subject != "checkpoint" {
					t.Fatalf("commits = %+v", git.Commits)
				}
				if len(git.Branches) != 2 || !git.Branches[0].Current {
					t.Fatalf("branches = %+v", git.Branches)
				}
				if git.Remote.Kind != "github" || git.CompareURL == "" {
					t.Fatalf("remote = %+v compare = %q", git.Remote, git.CompareURL)
				}
			},
		},
		{
			name: "models", path: "/api/models", body: modelsBody,
			call: func(c *Client) (any, error) { return c.Models(ctx) },
			check: func(t *testing.T, got any) {
				models := got.([]protocol.ModelEntry)
				if len(models) != 1 || models[0].ID != "sitin-deepseek/deepseek-flash" {
					t.Fatalf("models = %+v", models)
				}
				if !models[0].Reasoning || models[0].Cost.Input != 0.14 || len(models[0].ThinkingLevels) != 2 {
					t.Fatalf("model = %+v", models[0])
				}
			},
		},
		{
			name: "usage calendar", path: "/api/usage-calendar?days=120", body: usageBody,
			call: func(c *Client) (any, error) { return c.UsageCalendar(ctx, 120) },
			check: func(t *testing.T, got any) {
				days := got.([]protocol.UsageDay)
				if len(days) != 2 || days[0].Date != "2026-11-09" || days[1].Tokens != 40809588 {
					t.Fatalf("days = %+v", days)
				}
			},
		},
		{
			name: "access", path: "/api/access", body: accessBody,
			call: func(c *Client) (any, error) { return c.Access(ctx) },
			check: func(t *testing.T, got any) {
				access := got.(*protocol.AccessView)
				if len(access.Chats) != 2 || access.Chats[1].Level != protocol.AccessOwner {
					t.Fatalf("chats = %+v", access.Chats)
				}
				if access.Chats[1].Members != 3 || len(access.EnvChats) != 2 || access.OwnerChat == "" {
					t.Fatalf("access = %+v", access)
				}
			},
		},
		{
			name: "audit", path: "/api/audit?limit=40", body: auditBody,
			call: func(c *Client) (any, error) { return c.Audit(ctx, 40) },
			check: func(t *testing.T, got any) {
				events := got.([]protocol.AuditEvent)
				if len(events) != 2 || events[0].Kind != "deny" || events[1].Level != protocol.AccessOwner {
					t.Fatalf("events = %+v", events)
				}
			},
		},
		{
			name: "task", path: "/api/task?id=1084", body: taskBody,
			call: func(c *Client) (any, error) { return c.Task(ctx, 1084) },
			check: func(t *testing.T, got any) {
				task := got.(*protocol.TaskDetail)
				if task.ID != 1084 || !task.Background || task.Status != "sleeping" {
					t.Fatalf("task = %+v", task)
				}
				if task.Until == nil || *task.Until != 1794819765170 || task.Owner != nil || task.Phase != nil {
					t.Fatalf("task = %+v", task)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					t.Errorf("method = %s, want GET", r.Method)
				}
				if got := r.Header.Get("Authorization"); got != "Bearer "+token {
					t.Errorf("authorization = %q", got)
				}
				if got := r.Header.Get("Content-Type"); got != "application/json" {
					t.Errorf("content-type = %q", got)
				}
				if got := r.URL.RequestURI(); got != tt.path {
					t.Errorf("uri = %q, want %q", got, tt.path)
				}
				w.Header().Set("Content-Type", "application/json")
				io.WriteString(w, tt.body)
			}))
			defer server.Close()

			got, err := tt.call(New(server.URL, token))
			if err != nil {
				t.Fatalf("call: %v", err)
			}
			tt.check(t, got)
		})
	}
}

func TestClientPOST(t *testing.T) {
	const token = "s3cret"
	ctx := context.Background()

	tests := []struct {
		name  string
		path  string
		body  string
		reply string
		call  func(*Client) error
	}{
		{
			name: "add project", path: "/api/projects", body: `{"path":"/Users/me/work/pitta"}`,
			reply: `{"status":"added","project":{"id":"pitta","name":"pitta","path":"/Users/me/work/pitta"}}`,
			call:  func(c *Client) error { _, err := c.AddProject(ctx, "/Users/me/work/pitta"); return err },
		},
		{
			name: "remove project", path: "/api/projects/remove", body: `{"id":"pitta"}`,
			reply: `{"status":"removed","movedChats":1}`,
			call:  func(c *Client) error { _, err := c.RemoveProject(ctx, "pitta"); return err },
		},
		{
			name: "submit", path: "/api/submit",
			body:  `{"conversationId":2,"text":"hi","mode":"followUp","images":[{"data":"aGk=","mimeType":"image/png"}],"requestId":"r1"}`,
			reply: `{"submissionId":42}`,
			call: func(c *Client) error {
				return c.Submit(ctx, SubmitRequest{
					ConversationID: 2, Text: "hi", Mode: ModeFollowUp,
					Images: []Image{{Data: "aGk=", MimeType: "image/png"}}, RequestID: "r1",
				})
			},
		},
		{
			name: "abort", path: "/api/abort", body: `{"conversationId":2}`, reply: `{"ok":true}`,
			call: func(c *Client) error { return c.Abort(ctx, 2) },
		},
		{
			name: "reset", path: "/api/reset", body: `{"conversationId":2}`, reply: `{"ok":true}`,
			call: func(c *Client) error { return c.Reset(ctx, 2) },
		},
		{
			name: "commit", path: "/api/commit", body: `{"message":"checkpoint","conversationId":2}`,
			reply: `{"head":"9f2c1ab"}`,
			call: func(c *Client) error {
				head, err := c.Commit(ctx, 2, "checkpoint")
				if err == nil && head != "9f2c1ab" {
					t.Errorf("head = %q", head)
				}
				return err
			},
		},
		{
			name: "merge", path: "/api/merge", body: `{"conversationId":2}`,
			reply: `{"status":"conflict","branch":"agent/conv-2","base":"main","conflicts":["main.go"]}`,
			call: func(c *Client) error {
				result, err := c.Merge(ctx, 2)
				if err == nil && (result.Status != "conflict" || len(result.Conflicts) != 1) {
					t.Errorf("merge = %+v", result)
				}
				return err
			},
		},
		{
			name: "cancel task", path: "/api/task/cancel", body: `{"id":1084}`, reply: `{"ok":true}`,
			call: func(c *Client) error { return c.CancelTask(ctx, 1084) },
		},
		{
			name: "set model", path: "/api/model",
			body:  `{"conversationId":2,"provider":"magpie","modelId":"deepseek-flash","thinkingLevel":"high"}`,
			reply: `{"ok":true}`,
			call: func(c *Client) error {
				return c.SetModel(ctx, 2, ModelChoice{Provider: "magpie", ModelID: "deepseek-flash", ThinkingLevel: "high"})
			},
		},
		{
			name: "set default model", path: "/api/default-model",
			body:  `{"provider":"magpie","modelId":"deepseek-flash","thinkingLevel":"high"}`,
			reply: `{"ok":true}`,
			call: func(c *Client) error {
				return c.SetDefaultModel(ctx, ModelChoice{Provider: "magpie", ModelID: "deepseek-flash", ThinkingLevel: "high"})
			},
		},
		{
			name: "rename chat", path: "/api/surface-meta", body: `{"conversationId":2,"title":"renamed"}`,
			reply: `{"ok":true,"surfaceId":"cli"}`,
			call: func(c *Client) error {
				title := "renamed"
				return c.SetSurfaceMeta(ctx, SurfaceMeta{ConversationID: 2, Title: &title})
			},
		},
		{
			name: "remove worktree", path: "/api/worktree/remove", body: `{"conversationId":2}`,
			reply: `{"status":"removed","conversationId":2}`,
			call: func(c *Client) error {
				_, err := c.RemoveWorktree(ctx, 2)
				return err
			},
		},
		{
			name: "new conversation", path: "/api/conversations", body: `{"projectId":"workspace"}`,
			reply: `{"conversationId":7,"surfaceId":"client:abc"}`,
			call: func(c *Client) error {
				id, surface, err := c.NewConversation(ctx, "workspace")
				if err == nil && (id != 7 || surface != "client:abc") {
					t.Errorf("new conversation = %d %q", id, surface)
				}
				return err
			},
		},
		{
			name: "curate memory", path: "/api/memory/curate", body: `{"hours":24}`,
			reply: `{"suggestions":[{"text":"prefers conclusions","scope":"global","reason":"repeated"}],"scanned":12}`,
			call: func(c *Client) error {
				curation, err := c.CurateMemory(ctx, 24)
				if err == nil && (curation.Scanned != 12 || len(curation.Suggestions) != 1) {
					t.Errorf("curation = %+v", curation)
				}
				return err
			},
		},
		{
			name: "apply memory", path: "/api/memory/apply",
			body:  `{"suggestions":[{"text":"prefers conclusions","scope":"global","reason":""}]}`,
			reply: `{"added":1,"skipped":0}`,
			call: func(c *Client) error {
				return c.ApplyMemory(ctx, []protocol.MemorySuggestion{{Text: "prefers conclusions", Scope: "global"}})
			},
		},
		{
			name: "decide approval", path: "/api/approvals",
			body:  `{"callId":"c1","approved":true,"session":true,"conversationId":2}`,
			reply: `{"ok":true}`,
			call: func(c *Client) error {
				return c.DecideApproval(ctx, ApprovalDecision{CallID: "c1", Approved: true, Session: true, ConversationID: 2})
			},
		},
		{
			name: "set git base", path: "/api/git/base", body: `{"conversationId":2,"base":"main"}`,
			reply: `{"ok":true,"base":"main"}`,
			call:  func(c *Client) error { return c.SetGitBase(ctx, 2, "main") },
		},
		{
			name: "push", path: "/api/git/push", body: `{"conversationId":2,"dryRun":true}`,
			reply: `{"status":"dry-run","branch":"agent/conv-2","remote":"origin","output":"ok"}`,
			call: func(c *Client) error {
				result, err := c.Push(ctx, 2, true)
				if err == nil && (result.Status != "dry-run" || result.Output != "ok") {
					t.Errorf("push = %+v", result)
				}
				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost {
					t.Errorf("method = %s, want POST", r.Method)
				}
				if got := r.Header.Get("Authorization"); got != "Bearer "+token {
					t.Errorf("authorization = %q", got)
				}
				if got := r.URL.RequestURI(); got != tt.path {
					t.Errorf("uri = %q, want %q", got, tt.path)
				}
				var got, want any
				raw, err := io.ReadAll(r.Body)
				if err != nil {
					t.Fatalf("read body: %v", err)
				}
				if err := json.Unmarshal(raw, &got); err != nil {
					t.Fatalf("body %s: %v", raw, err)
				}
				if err := json.Unmarshal([]byte(tt.body), &want); err != nil {
					t.Fatalf("want body: %v", err)
				}
				if !reflect.DeepEqual(got, want) {
					t.Errorf("body = %s, want %s", raw, tt.body)
				}
				w.Header().Set("Content-Type", "application/json")
				io.WriteString(w, tt.reply)
			}))
			defer server.Close()

			if err := tt.call(New(server.URL, token)); err != nil {
				t.Fatalf("call: %v", err)
			}
		})
	}
}

func TestClientStatusError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "" {
			t.Errorf("authorization = %q, want none without a token", got)
		}
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer server.Close()

	_, err := New(server.URL, "").State(context.Background())
	if err == nil {
		t.Fatal("State: want error for 500")
	}
	const want = "GET /api/state → 500 Internal Server Error"
	if err.Error() != want {
		t.Fatalf("error = %q, want %q", err, want)
	}
	var status *StatusError
	if !errors.As(err, &status) {
		t.Fatalf("error type = %T, want *StatusError", err)
	}
	if status.Code != http.StatusInternalServerError || status.Method != http.MethodGet || status.Path != "/api/state" {
		t.Fatalf("status = %+v", status)
	}
}

func TestClientBaseURLTrailingSlash(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Path; got != "/api/state" {
			t.Errorf("path = %q", got)
		}
		io.WriteString(w, stateBody)
	}))
	defer server.Close()

	if _, err := New(server.URL+"/", "").State(context.Background()); err != nil {
		t.Fatalf("State: %v", err)
	}
}
