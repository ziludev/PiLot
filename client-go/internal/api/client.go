// Package api is the Go client of the local pi-durable-agent bridge. It speaks
// only the endpoints the desktop window needs: the rail's snapshot, one chat's
// page, its newest entry, task titles, and the three writes (submit, abort,
// approval).
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"pitta/internal/protocol"
)

// Client talks to one bridge.
type Client struct {
	baseURL string
	token   string
	http    *http.Client
}

// New returns a client for baseURL (http://127.0.0.1:8787); token may be empty.
func New(baseURL, token string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		http:    &http.Client{},
	}
}

// StatusError is a non-2xx reply.
type StatusError struct {
	Method string
	Path   string
	Code   int
	Status string
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("%s %s → %d %s", e.Method, e.Path, e.Code, e.Status)
}

func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	var payload io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return err
		}
		payload = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, payload)
	if err != nil {
		return err
	}
	req.Header.Set("content-type", "application/json")
	if c.token != "" {
		req.Header.Set("authorization", "Bearer "+c.token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		// Drain so the connection can be reused instead of being reset.
		io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
		return &StatusError{Method: method, Path: path, Code: resp.StatusCode, Status: statusText(resp)}
	}
	if out == nil {
		_, err := io.Copy(io.Discard, resp.Body)
		return err
	}
	// Unknown fields are expected: the bridge ships new ones before this client
	// knows them, so the decoder's default leniency is the contract.
	return json.NewDecoder(resp.Body).Decode(out)
}

func statusText(resp *http.Response) string {
	text := strings.TrimSpace(strings.TrimPrefix(resp.Status, strconv.Itoa(resp.StatusCode)))
	if text == "" {
		text = http.StatusText(resp.StatusCode)
	}
	return text
}

func (c *Client) get(ctx context.Context, path string, out any) error {
	return c.do(ctx, http.MethodGet, path, nil, out)
}

func (c *Client) post(ctx context.Context, path string, body, out any) error {
	return c.do(ctx, http.MethodPost, path, body, out)
}

// State is the rail's snapshot: conversations, and what the agent is doing.
func (c *Client) State(ctx context.Context) (*protocol.State, error) {
	var state protocol.State
	if err := c.get(ctx, "/api/state", &state); err != nil {
		return nil, err
	}
	return &state, nil
}

// Tasks reads the task list. /api/state's tasks carry no title, so the rail
// reads them here instead.
func (c *Client) Tasks(ctx context.Context) ([]protocol.Task, error) {
	var reply struct {
		Tasks []protocol.Task `json:"tasks"`
	}
	if err := c.get(ctx, "/api/tasks", &reply); err != nil {
		return nil, err
	}
	return reply.Tasks, nil
}

// conversationPage is how many entries one read asks for: enough to fill the
// transcript, few enough that a page stays cheap to merge.
const conversationPage = 120

// Conversation reads the newest page of one chat.
func (c *Client) Conversation(ctx context.Context, conversationID int) (*protocol.Conversation, error) {
	return c.ConversationBefore(ctx, conversationID, 0)
}

// ConversationBefore reads the page of entries older than before, or the newest
// page when before is 0. Paging backwards is how a long chat is read whole: the
// newest page arrives first and the rest is fetched as the reader walks up.
func (c *Client) ConversationBefore(ctx context.Context, conversationID, before int) (*protocol.Conversation, error) {
	var conversation protocol.Conversation
	path := fmt.Sprintf("/api/conversation?id=%d&limit=%d", conversationID, conversationPage)
	if before > 0 {
		path += fmt.Sprintf("&before=%d", before)
	}
	if err := c.get(ctx, path, &conversation); err != nil {
		return nil, err
	}
	return &conversation, nil
}

// ConversationTail reads the newest single entry, which is all the rail needs
// to place a conversation in time.
func (c *Client) ConversationTail(ctx context.Context, conversationID int) (*protocol.Conversation, error) {
	var conversation protocol.Conversation
	path := fmt.Sprintf("/api/conversation?id=%d&limit=1", conversationID)
	if err := c.get(ctx, path, &conversation); err != nil {
		return nil, err
	}
	return &conversation, nil
}

// SubmitMode tells the bridge whether to queue behind a running turn or steer it.
type SubmitMode string

const (
	ModeFollowUp SubmitMode = "followUp"
	ModeSteer    SubmitMode = "steer"
)

// Image is one base64 attachment on a submission.
type Image struct {
	Data     string `json:"data"`
	MimeType string `json:"mimeType"`
}

// SubmitRequest is one user turn.
type SubmitRequest struct {
	ConversationID int        `json:"conversationId"`
	Text           string     `json:"text"`
	Mode           SubmitMode `json:"mode"`
	Images         []Image    `json:"images,omitempty"`
	// RequestID dedupes a retried submission.
	RequestID string `json:"requestId,omitempty"`
}

// Submit sends one turn to a chat.
func (c *Client) Submit(ctx context.Context, req SubmitRequest) error {
	return c.post(ctx, "/api/submit", req, nil)
}

// AbortOutcome is what POST /api/abort did: the bridge decides whether a stop
// means "that message I just sent, not yet started" or "the work in flight".
type AbortOutcome struct {
	Cancelled int  `json:"cancelled"`
	Aborted   bool `json:"aborted"`
}

// Abort stops the run in flight, or takes a message still waiting in line out
// of it. all asks for both: the desktop's stop button clears the line behind
// the run rather than leaving it to start the next turn.
func (c *Client) Abort(ctx context.Context, conversationID int, all bool) (AbortOutcome, error) {
	var outcome AbortOutcome
	body := struct {
		ConversationID int  `json:"conversationId"`
		All            bool `json:"all,omitempty"`
	}{ConversationID: conversationID, All: all}
	err := c.post(ctx, "/api/abort", body, &outcome)
	return outcome, err
}

// doAny decodes the reply whatever the status code says: the projects
// endpoints refuse with the reason in the body ("not a repo", "missing"), and a
// window that shows "400 Bad Request" instead of the reason is a window that
// made the person guess.
func (c *Client) doAny(ctx context.Context, method, path string, body, out any) error {
	var payload io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return err
		}
		payload = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, payload)
	if err != nil {
		return err
	}
	req.Header.Set("content-type", "application/json")
	if c.token != "" {
		req.Header.Set("authorization", "Bearer "+c.token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			if resp.StatusCode >= 200 && resp.StatusCode <= 299 {
				return err
			}
			return &StatusError{Method: method, Path: path, Code: resp.StatusCode, Status: statusText(resp)}
		}
		return nil
	}
	_, err = io.Copy(io.Discard, resp.Body)
	return err
}

// Projects lists the repositories the bridge knows, the built-in sandbox first.
func (c *Client) Projects(ctx context.Context) ([]protocol.Project, error) {
	var out struct {
		Projects []protocol.Project `json:"projects"`
	}
	if err := c.get(ctx, "/api/projects", &out); err != nil {
		return nil, err
	}
	return out.Projects, nil
}

// AddProject registers a repository by path. It is never initialised: a folder
// that is not a repo is refused, and the refusal is the answer.
func (c *Client) AddProject(ctx context.Context, path string) (protocol.ProjectOpen, error) {
	var out protocol.ProjectOpen
	err := c.doAny(ctx, http.MethodPost, "/api/projects",
		struct {
			Path string `json:"path"`
		}{Path: path}, &out)
	return out, err
}

// RemoveProject forgets a project. Its chats move to the built-in sandbox; the
// repository on disk is never touched.
func (c *Client) RemoveProject(ctx context.Context, id string) (protocol.ProjectRemoved, error) {
	var out protocol.ProjectRemoved
	err := c.doAny(ctx, http.MethodPost, "/api/projects/remove",
		struct {
			ID string `json:"id"`
		}{ID: id}, &out)
	return out, err
}

// NewConversation starts a chat in projectID (empty uses the built-in workspace).
func (c *Client) NewConversation(ctx context.Context, projectID string) (conversationID int, surfaceID string, err error) {
	var reply struct {
		ConversationID int    `json:"conversationId"`
		SurfaceID      string `json:"surfaceId"`
	}
	body := struct {
		ProjectID string `json:"projectId,omitempty"`
	}{ProjectID: projectID}
	if err := c.post(ctx, "/api/conversations", body, &reply); err != nil {
		return 0, "", err
	}
	return reply.ConversationID, reply.SurfaceID, nil
}

// ApprovalDecision answers one pending tool call. The protocol has no
// conversation on a decision, so the call id is the whole address.
type ApprovalDecision struct {
	CallID   string `json:"callId"`
	Approved bool   `json:"approved"`
	Session  bool   `json:"session,omitempty"`
	All      bool   `json:"all,omitempty"`
}

// DecideApproval approves or rejects a pending tool call.
func (c *Client) DecideApproval(ctx context.Context, decision ApprovalDecision) error {
	return c.post(ctx, "/api/approvals", decision, nil)
}

// Approvals lists the tool calls waiting for a decision, across every chat:
// the rail warns about them before the user opens one.
func (c *Client) Approvals(ctx context.Context) ([]protocol.PendingApproval, error) {
	var reply struct {
		Approvals []protocol.PendingApproval `json:"approvals"`
	}
	if err := c.get(ctx, "/api/approvals", &reply); err != nil {
		return nil, err
	}
	return reply.Approvals, nil
}

// Health is the bridge's own report: whether the agent runs, and how much work
// is waiting. The composer shows the queue depth from it.
func (c *Client) Health(ctx context.Context) (*protocol.Health, error) {
	var health protocol.Health
	if err := c.get(ctx, "/api/health", &health); err != nil {
		return nil, err
	}
	return &health, nil
}

// Search looks inside transcript text across chats (GET /api/search). The
// bridge caps the reply itself — thirty hits, recent chats only — so there is
// no limit or cursor to pass.
func (c *Client) Search(ctx context.Context, q string) ([]protocol.SearchHit, error) {
	var reply struct {
		Hits []protocol.SearchHit `json:"hits"`
	}
	if err := c.get(ctx, "/api/search?q="+url.QueryEscape(q), &reply); err != nil {
		return nil, err
	}
	return reply.Hits, nil
}

// SurfaceMeta is one write to a chat's surface metadata. A nil field is left
// alone; a title that trims to nothing clears the override, so the rail falls
// back to the chat's first message.
type SurfaceMeta struct {
	ConversationID int     `json:"conversationId"`
	Title          *string `json:"title,omitempty"`
	Archived       *bool   `json:"archived,omitempty"`
}

// Models is the whole catalog, with the default a new chat starts on.
func (c *Client) Models(ctx context.Context) (*protocol.Models, error) {
	var out protocol.Models
	if err := c.get(ctx, "/api/models", &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// SetModel points one chat at a model, and at a thinking level when the model
// takes one. An empty level leaves the chat's level as the bridge has it.
func (c *Client) SetModel(ctx context.Context, convID int, ref protocol.ModelRef, level string) error {
	body := struct {
		ConversationID int    `json:"conversationId"`
		Provider       string `json:"provider"`
		ModelID        string `json:"modelId"`
		ThinkingLevel  string `json:"thinkingLevel,omitempty"`
	}{convID, ref.Provider, ref.ModelID, level}
	return c.post(ctx, "/api/model", body, nil)
}

// PatchSurface writes a chat's title or archive flag (POST /api/surface-meta).
func (c *Client) PatchSurface(ctx context.Context, meta SurfaceMeta) error {
	return c.post(ctx, "/api/surface-meta", meta, nil)
}

// Memory is GET /api/memory: the three note lists the memory panel shows. The
// chat list belongs to one conversation, so the panel asks again when the open
// chat changes.
func (c *Client) Memory(ctx context.Context, convID int) (*protocol.Memory, error) {
	path := "/api/memory"
	if convID > 0 {
		path += "?chat=" + strconv.Itoa(convID)
	}
	var out protocol.Memory
	if err := c.get(ctx, path, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CurateMemory asks the bridge to read the last hours of transcripts and
// propose memory lines. It is a model call: slow, and the reply says so with
// its own Error when the pass failed.
func (c *Client) CurateMemory(ctx context.Context, hours int) (*protocol.Curation, error) {
	body := struct {
		Hours int `json:"hours"`
	}{hours}
	var out protocol.Curation
	if err := c.post(ctx, "/api/memory/curate", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ApplyMemory writes the lines the user kept.
func (c *Client) ApplyMemory(ctx context.Context, lines []protocol.MemorySuggestion) (*protocol.MemoryApplied, error) {
	body := struct {
		Suggestions []protocol.MemorySuggestion `json:"suggestions"`
	}{lines}
	var out protocol.MemoryApplied
	if err := c.post(ctx, "/api/memory/apply", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Changes is GET /api/changes: the worktree's uncommitted files and their patch.
// The patch is the whole thing; splitting it per file is the panel's job, because
// only the panel knows which file is open.
func (c *Client) Changes(ctx context.Context, convID int) (*protocol.Changes, error) {
	var out protocol.Changes
	path := "/api/changes?conversationId=" + strconv.Itoa(convID)
	if err := c.get(ctx, path, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Git is GET /api/git: the branch, its commits and where they went.
func (c *Client) Git(ctx context.Context, convID, limit int) (*protocol.Git, error) {
	var out protocol.Git
	path := "/api/git?conversationId=" + strconv.Itoa(convID) + "&limit=" + strconv.Itoa(limit)
	if err := c.get(ctx, path, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CommitResult is what POST /api/commit answers: the commit it made.
type CommitResult struct {
	Head string `json:"head"`
}

// Commit records everything in the worktree as one commit.
func (c *Client) Commit(ctx context.Context, convID int, message string) (CommitResult, error) {
	var out CommitResult
	err := c.post(ctx, "/api/commit", struct {
		ConversationID int    `json:"conversationId"`
		Message        string `json:"message"`
	}{ConversationID: convID, Message: message}, &out)
	return out, err
}

// PushResult is one of four answers from POST /api/git/push. A dry run is not a
// failure: the bridge refuses to force, and says what it would send first.
type PushResult struct {
	Status     string `json:"status"` // pushed | dry-run | no-remote | error
	Branch     string `json:"branch"`
	Remote     string `json:"remote"`
	CompareURL string `json:"compareUrl"`
	Output     string `json:"output"`
	Message    string `json:"message"`
}

// Push sends the chat's branch to origin, never forced.
func (c *Client) Push(ctx context.Context, convID int, dryRun bool) (PushResult, error) {
	var out PushResult
	err := c.post(ctx, "/api/git/push", struct {
		ConversationID int  `json:"conversationId"`
		DryRun         bool `json:"dryRun"`
	}{ConversationID: convID, DryRun: dryRun}, &out)
	return out, err
}

// MergeResult is what POST /api/merge answers. The bridge would rather refuse
// than guess: both sides must be committed, and a conflict is undone and
// reported with the files that caused it.
type MergeResult struct {
	Status    string   `json:"status"` // merged | up-to-date | dirty | main-dirty | conflict | error
	Branch    string   `json:"branch"`
	Base      string   `json:"base"`
	Commits   int      `json:"commits"`
	Files     []string `json:"files"`
	Conflicts []string `json:"conflicts"`
	Message   string   `json:"message"`
}

// Merge merges the chat's branch back into the base branch.
func (c *Client) Merge(ctx context.Context, convID int) (MergeResult, error) {
	var out MergeResult
	err := c.post(ctx, "/api/merge", struct {
		ConversationID int `json:"conversationId"`
	}{ConversationID: convID}, &out)
	return out, err
}

// TaskDetail is one task as GET /api/task describes it: what it is doing, what
// it was asked, and what it spawned.
type TaskDetail struct {
	ID             int    `json:"id"`
	Kind           string `json:"kind"`
	ConversationID int    `json:"conversationId"`
	// The task that owns this one — a task id, or null for work nobody owns. Read as a
	// number: the bridge sends what the harness records, and a string here failed the whole
	// detail with "cannot unmarshal number into Go struct field TaskDetail.owner".
	Owner          *int            `json:"owner"`
	Background     bool            `json:"background"`
	AbortRequested bool            `json:"abortRequested"`
	Status         string          `json:"status"`
	Phase          string          `json:"phase"`
	Until          int64           `json:"until"`
	Title          string          `json:"title"`
	Input          json.RawMessage `json:"input"`
	Outcome        json.RawMessage `json:"outcome"`
	SubAgents      []TaskSubAgent  `json:"subAgents"`
	StartedAt      int64           `json:"startedAt"`
}

// TaskSubAgent is one child a task spawned, which is where a running task's
// signs of life are. The conversation is how the child's steps are found.
type TaskSubAgent struct {
	ID             string `json:"id"`
	Kind           string `json:"kind"`
	Title          string `json:"title"`
	State          string `json:"state"`
	ConversationID int    `json:"conversationId"`
}

// TaskLogStep is one thing a task did: a tool call with its verdict, or a reply
// in words. Thinking is not kept — it is the model talking to itself.
type TaskLogStep struct {
	ID    int    `json:"id"`
	Kind  string `json:"kind"`
	At    int64  `json:"at"`
	Tool  string `json:"tool"`
	Label string `json:"label"`
	// Args is the whole call, pretty-printed; Label is its first line. The row
	// draws Label and opens Args underneath it.
	Args string `json:"args"`
	// Output is what the call answered, cut to a screenful by the bridge. Empty
	// while it runs, and for calls that answered with nothing.
	Output string `json:"output"`
	State  string `json:"state"`
	Text   string `json:"text"`
}

// TaskLogStage is one conversation's part of a run: the task's own turn when it
// spawned nothing, each child when it did.
type TaskLogStage struct {
	ConversationID int  `json:"conversationId"`
	Parent         bool `json:"parent"`
	// Start marks the one row that began the task: the call that made it. It
	// belongs to the turn above, not to the task, so it is its own stage.
	Start bool          `json:"start"`
	Role  string        `json:"role"`
	Steps []TaskLogStep `json:"steps"`
	More  int           `json:"more"`
}

// TaskLog reads what the task did, stage by stage. A task that ran for hours can
// break at 03:40, and the only way to see which command went red used to be
// opening the child's chat — which is not in the rail at all.
func (c *Client) TaskLog(ctx context.Context, id int) ([]TaskLogStage, error) {
	var log struct {
		Stages []TaskLogStage `json:"stages"`
	}
	err := c.get(ctx, "/api/task/log?id="+strconv.Itoa(id), &log)
	return log.Stages, err
}

// Task reads one task in detail.
func (c *Client) Task(ctx context.Context, id int) (TaskDetail, error) {
	var task TaskDetail
	err := c.get(ctx, "/api/task?id="+strconv.Itoa(id), &task)
	return task, err
}

// CancelTask asks the bridge to stop a running or waiting task.
func (c *Client) CancelTask(ctx context.Context, id int) error {
	return c.post(ctx, "/api/task/cancel", struct {
		ID int `json:"id"`
	}{ID: id}, nil)
}

// RescheduleTask moves a routine to a new schedule. A sleeping task's deadline
// belongs to the sleep it is in, so the bridge replaces the task and answers
// with the id of the one that now carries the schedule; the old id is dead.
func (c *Client) RescheduleTask(ctx context.Context, id int, schedule string) (int, error) {
	var reply struct {
		ID int `json:"id"`
	}
	err := c.post(ctx, "/api/task/reschedule", struct {
		ID       int    `json:"id"`
		Schedule string `json:"schedule"`
	}{ID: id, Schedule: schedule}, &reply)
	return reply.ID, err
}

// UsageDay is one day of spend, oldest first in the calendar.
type UsageDay struct {
	Date   string  `json:"date"`
	Cost   float64 `json:"cost"`
	Tokens int64   `json:"tokens"`
}

// UsageCalendar is the daily spend over the last days, for the heat map.
func (c *Client) UsageCalendar(ctx context.Context, days int) ([]UsageDay, error) {
	var reply struct {
		Days []UsageDay `json:"days"`
	}
	if err := c.get(ctx, "/api/usage-calendar?days="+strconv.Itoa(days), &reply); err != nil {
		return nil, err
	}
	return reply.Days, nil
}

// FileText is one file as GET /api/file hands it over: the text, whether it was
// cut short, and how big it is on disk.
type FileText struct {
	Path      string `json:"path"`
	Size      int64  `json:"size"`
	Truncated bool   `json:"truncated"`
	Lines     int    `json:"lines"`
	Content   string `json:"content"`
}

// File reads a file from the chat's workspace, for the preview beside a patch:
// a diff says what changed, and sometimes the question is what the file says
// now.
func (c *Client) File(ctx context.Context, convID int, path string) (FileText, error) {
	var out FileText
	query := "/api/file?conversationId=" + strconv.Itoa(convID) + "&path=" + url.QueryEscape(path)
	err := c.get(ctx, query, &out)
	return out, err
}
