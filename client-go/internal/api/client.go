// Package api is the Go client of the local pi-durable-agent bridge: the same
// HTTP paths and socket the earlier web client (the old client/lib/api.ts,
// the old client/the old client's store) uses.
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

// StatusError is a non-2xx reply. Its message matches the earlier web client's
// "GET /api/state → 500 Internal Server Error".
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

// State is the rail, its tasks and the project list.
func (c *Client) State(ctx context.Context) (*protocol.State, error) {
	var state protocol.State
	if err := c.get(ctx, "/api/state", &state); err != nil {
		return nil, err
	}
	return &state, nil
}

// Health asks the bridge how it is. The error is the answer that matters most:
// a failed request is how the footer learns the agent is not there.
func (c *Client) Health(ctx context.Context) (*protocol.Health, error) {
	var health protocol.Health
	if err := c.get(ctx, "/api/health", &health); err != nil {
		return nil, err
	}
	return &health, nil
}

// Conversation reads the newest page of one chat.
func (c *Client) Conversation(ctx context.Context, conversationID int) (*protocol.Conversation, error) {
	return c.conversation(ctx, conversationID, 0)
}

// ConversationBefore reads the page that ends just before entry `before`, so a
// chat longer than one page can be read back to its first message.
func (c *Client) ConversationBefore(ctx context.Context, conversationID, before int) (*protocol.Conversation, error) {
	return c.conversation(ctx, conversationID, before)
}

// conversationPage is how many entries one request asks for: enough turns to
// fill the transcript's window, few enough that the frame stays cheap.
const conversationPage = 200

func (c *Client) conversation(ctx context.Context, conversationID, before int) (*protocol.Conversation, error) {
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

// Memory reads the three memory scopes; chatID 0 asks for global and work only.
func (c *Client) Memory(ctx context.Context, chatID int) (*protocol.Memory, error) {
	path := "/api/memory"
	if chatID > 0 {
		path = fmt.Sprintf("/api/memory?chat=%d", chatID)
	}
	var memory protocol.Memory
	if err := c.get(ctx, path, &memory); err != nil {
		return nil, err
	}
	return &memory, nil
}

// Changes reads the worktree diff; conversationID 0 means the shared workspace.
func (c *Client) Changes(ctx context.Context, conversationID int) (*protocol.Changes, error) {
	path := "/api/changes"
	if conversationID > 0 {
		path = fmt.Sprintf("/api/changes?conversationId=%d", conversationID)
	}
	var changes protocol.Changes
	if err := c.get(ctx, path, &changes); err != nil {
		return nil, err
	}
	return &changes, nil
}

// Git reads the branch view of one chat's worktree.
func (c *Client) Git(ctx context.Context, conversationID int) (*protocol.Git, error) {
	var git protocol.Git
	path := fmt.Sprintf("/api/git?conversationId=%d", conversationID)
	if err := c.get(ctx, path, &git); err != nil {
		return nil, err
	}
	return &git, nil
}

// Models lists the models the bridge can run.
func (c *Client) Models(ctx context.Context) ([]protocol.ModelEntry, error) {
	var reply struct {
		Models []protocol.ModelEntry `json:"models"`
	}
	if err := c.get(ctx, "/api/models", &reply); err != nil {
		return nil, err
	}
	return reply.Models, nil
}

// UsageCalendar reads up to days of spend for the calendar.
func (c *Client) UsageCalendar(ctx context.Context, days int) ([]protocol.UsageDay, error) {
	var reply struct {
		Days []protocol.UsageDay `json:"days"`
	}
	if err := c.get(ctx, fmt.Sprintf("/api/usage-calendar?days=%d", days), &reply); err != nil {
		return nil, err
	}
	return reply.Days, nil
}

// Access reads who may use which chat.
func (c *Client) Access(ctx context.Context) (*protocol.AccessView, error) {
	var access protocol.AccessView
	if err := c.get(ctx, "/api/access", &access); err != nil {
		return nil, err
	}
	return &access, nil
}

// Audit reads the newest limit access decisions.
func (c *Client) Audit(ctx context.Context, limit int) ([]protocol.AuditEvent, error) {
	var reply struct {
		Events []protocol.AuditEvent `json:"events"`
	}
	if err := c.get(ctx, fmt.Sprintf("/api/audit?limit=%d", limit), &reply); err != nil {
		return nil, err
	}
	return reply.Events, nil
}

// Task reads one background or sleeping task.
func (c *Client) Task(ctx context.Context, id int) (*protocol.TaskDetail, error) {
	var detail protocol.TaskDetail
	if err := c.get(ctx, fmt.Sprintf("/api/task?id=%d", id), &detail); err != nil {
		return nil, err
	}
	return &detail, nil
}

// AddProject registers a working directory with the bridge.
func (c *Client) AddProject(ctx context.Context, path string) (*protocol.ProjectOpen, error) {
	var reply protocol.ProjectOpen
	if err := c.post(ctx, "/api/projects", struct {
		Path string `json:"path"`
	}{Path: path}, &reply); err != nil {
		return nil, err
	}
	return &reply, nil
}

// RemoveProject unregisters a project by id.
func (c *Client) RemoveProject(ctx context.Context, id string) (*protocol.ProjectRemoved, error) {
	var reply protocol.ProjectRemoved
	if err := c.post(ctx, "/api/projects/remove", struct {
		ID string `json:"id"`
	}{ID: id}, &reply); err != nil {
		return nil, err
	}
	return &reply, nil
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
// of it. The outcome says which, so the window can say it too.
func (c *Client) Abort(ctx context.Context, conversationID int) (AbortOutcome, error) {
	var outcome AbortOutcome
	err := c.post(ctx, "/api/abort", conversationIDBody(conversationID), &outcome)
	return outcome, err
}

// Reset clears the chat's context.
func (c *Client) Reset(ctx context.Context, conversationID int) error {
	return c.post(ctx, "/api/reset", conversationIDBody(conversationID), nil)
}

// File reads workspace file content for the viewer.
func (c *Client) File(ctx context.Context, path string, conversationID int) (*protocol.FilePayload, error) {
	target := "/api/file?path=" + url.QueryEscape(path)
	if conversationID != 0 {
		target += "&conversationId=" + strconv.Itoa(conversationID)
	}
	var payload protocol.FilePayload
	if err := c.get(ctx, target, &payload); err != nil {
		return nil, err
	}
	return &payload, nil
}

// CommitDiff is the unified diff one commit introduced.
func (c *Client) CommitDiff(ctx context.Context, conversationID int, hash string) (string, error) {
	target := fmt.Sprintf("/api/git/diff?conversationId=%d&hash=%s", conversationID, url.QueryEscape(hash))
	var raw json.RawMessage
	if err := c.get(ctx, target, &raw); err != nil {
		return "", err
	}
	// The bridge answers either a JSON string or {"diff": "..."}; accept both.
	var diff string
	if err := json.Unmarshal(raw, &diff); err == nil {
		return diff, nil
	}
	var wrapped struct {
		Diff string `json:"diff"`
	}
	if err := json.Unmarshal(raw, &wrapped); err != nil {
		return "", fmt.Errorf("commit diff %s: %w", hash, err)
	}
	return wrapped.Diff, nil
}

// Commit checkpoints the worktree and returns the new head hash.
func (c *Client) Commit(ctx context.Context, conversationID int, message string) (string, error) {
	var reply struct {
		Head string `json:"head"`
	}
	body := struct {
		Message        string `json:"message,omitempty"`
		ConversationID int    `json:"conversationId"`
	}{Message: message, ConversationID: conversationID}
	if err := c.post(ctx, "/api/commit", body, &reply); err != nil {
		return "", err
	}
	return reply.Head, nil
}

// CancelTask aborts a background or sleeping task.
func (c *Client) CancelTask(ctx context.Context, id int) error {
	return c.post(ctx, "/api/task/cancel", struct {
		ID int `json:"id"`
	}{ID: id}, nil)
}

// ModelChoice is the model and thinking level to run.
type ModelChoice struct {
	Provider      string `json:"provider"`
	ModelID       string `json:"modelId"`
	ThinkingLevel string `json:"thinkingLevel,omitempty"`
}

// SetModel switches the model of one chat.
func (c *Client) SetModel(ctx context.Context, conversationID int, choice ModelChoice) error {
	body := struct {
		ConversationID int `json:"conversationId"`
		ModelChoice
	}{ConversationID: conversationID, ModelChoice: choice}
	return c.post(ctx, "/api/model", body, nil)
}

// SetDefaultModel changes the model new chats start with.
func (c *Client) SetDefaultModel(ctx context.Context, choice ModelChoice) error {
	return c.post(ctx, "/api/default-model", choice, nil)
}

// SurfaceMeta is the rail metadata POST /api/surface-meta changes. A nil Title
// or Archived leaves that field alone; a pointer to "" clears the title.
type SurfaceMeta struct {
	ConversationID int `json:"conversationId,omitempty"`
	// SurfaceID addresses the surface directly; the bridge cannot always
	// resolve a conversation to one, and answers 400 when neither works.
	SurfaceID string  `json:"surfaceId,omitempty"`
	Title     *string `json:"title,omitempty"`
	Archived  *bool   `json:"archived,omitempty"`
}

// SetSurfaceMeta renames or archives a chat.
func (c *Client) SetSurfaceMeta(ctx context.Context, meta SurfaceMeta) error {
	return c.post(ctx, "/api/surface-meta", meta, nil)
}

// RemoveWorktree deletes the chat's checkout, keeping its transcript. A chat
// whose branch still holds unmerged commits answers "unmerged" instead of
// deleting anything, which the caller has to say out loud.
func (c *Client) RemoveWorktree(ctx context.Context, conversationID int) (*protocol.WorktreeRemoved, error) {
	var out protocol.WorktreeRemoved
	if err := c.post(ctx, "/api/worktree/remove", conversationIDBody(conversationID), &out); err != nil {
		return nil, err
	}
	return &out, nil
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

// Curation is the reply of POST /api/memory/curate.
type Curation struct {
	Suggestions []protocol.MemorySuggestion `json:"suggestions"`
	Scanned     int                         `json:"scanned"`
}

// CurateMemory proposes memory lines from the last hours of transcripts.
func (c *Client) CurateMemory(ctx context.Context, hours int) (*Curation, error) {
	var curation Curation
	body := struct {
		Hours int `json:"hours"`
	}{Hours: hours}
	if err := c.post(ctx, "/api/memory/curate", body, &curation); err != nil {
		return nil, err
	}
	return &curation, nil
}

// ApplyMemory stores the accepted suggestions.
func (c *Client) ApplyMemory(ctx context.Context, suggestions []protocol.MemorySuggestion) error {
	body := struct {
		Suggestions []protocol.MemorySuggestion `json:"suggestions"`
	}{Suggestions: suggestions}
	return c.post(ctx, "/api/memory/apply", body, nil)
}

// ApprovalDecision answers one pending tool call. Session extends the grant to
// the rest of the conversation when the bridge offered a grantKey.
type ApprovalDecision struct {
	CallID         string `json:"callId"`
	Approved       bool   `json:"approved"`
	Session        bool   `json:"session,omitempty"`
	ConversationID int    `json:"conversationId,omitempty"`
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

// Search looks for text inside transcripts across conversations. The bridge
// scans the newest conversations, so an old chat may not be in the result.
func (c *Client) Search(ctx context.Context, query string) ([]protocol.SearchHit, error) {
	var out struct {
		Hits []protocol.SearchHit `json:"hits"`
	}
	path := "/api/search?q=" + url.QueryEscape(query)
	if err := c.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return out.Hits, nil
}

// SetGitBase changes the branch this chat merges into.
func (c *Client) SetGitBase(ctx context.Context, conversationID int, base string) error {
	body := struct {
		ConversationID int    `json:"conversationId"`
		Base           string `json:"base"`
	}{ConversationID: conversationID, Base: base}
	return c.post(ctx, "/api/git/base", body, nil)
}

// Push pushes the chat's branch; dryRun reports what would happen.
func (c *Client) Push(ctx context.Context, conversationID int, dryRun bool) (*protocol.PushResult, error) {
	var result protocol.PushResult
	body := struct {
		ConversationID int  `json:"conversationId"`
		DryRun         bool `json:"dryRun"`
	}{ConversationID: conversationID, DryRun: dryRun}
	if err := c.post(ctx, "/api/git/push", body, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func conversationIDBody(conversationID int) any {
	return struct {
		ConversationID int `json:"conversationId"`
	}{ConversationID: conversationID}
}
