// Package protocol holds the wire shapes of the agent bridge, as the earlier client
// client declared them in the old client/lib/api.ts and the old client/the old client's store.
//
// Entries are the one shape the backend keeps extending: their Data (and
// Model) stay raw JSON here so that a field this client does not know yet
// cannot break the decode. The views parse what they understand.
package protocol

import "encoding/json"

// Entry is one transcript row. Kind names the payload in Data.
type Entry struct {
	ID    int             `json:"id"`
	Kind  string          `json:"kind"`
	Model json.RawMessage `json:"model"`
	Data  json.RawMessage `json:"data"`
}

// Surface is one conversation as the rail lists it.
type Surface struct {
	SurfaceID      string  `json:"surfaceId"`
	ConversationID int     `json:"conversationId"`
	Kind           string  `json:"kind"`
	Cost           float64 `json:"cost"`
	Tokens         int     `json:"tokens"`
	ThreadKey      string  `json:"threadKey"`
	ThreadParent   int     `json:"threadParent"`
	Title          string  `json:"title"`
	Archived       bool    `json:"archived"`
	ProjectID      string  `json:"projectId"`
	// LastStop is how the chat's newest assistant reply ended when that was an
	// error or an abort. A finished answer leaves it empty, which is what tells a
	// row that needs its owner apart from one that merely answered.
	LastStop string `json:"lastStop"`
}

// Health is GET /api/health: what the bridge says about itself. The footer
// shows it next to the workspace path, so that a bridge which died and came
// back is something the user can see instead of guess at.
type Health struct {
	PID        int    `json:"pid"`
	StartedAt  string `json:"startedAt"` // ISO
	UptimeMS   int64  `json:"uptimeMs"`
	Version    string `json:"version"`
	QueueDepth int    `json:"queueDepth"`
	Running    int    `json:"runningTurns"`
	Chats      int    `json:"conversations"`
}

// Task is a background or sleeping task the agent runs.
type Task struct {
	ID             int    `json:"id"`
	Kind           string `json:"kind"`
	ConversationID int    `json:"conversationId"`
	Background     bool   `json:"background"`
	State          string `json:"state"`
	Until          int64  `json:"until"`
	Title          string `json:"title"`
	// Spend is what the task's last run cost, for tasks that run unattended.
	Spend *TaskSpend `json:"spend"`
}

// TaskSpend is one run's price, as the bridge measured it around the turn.
type TaskSpend struct {
	Cost   float64 `json:"cost"`
	Tokens int     `json:"tokens"`
}

// ProjectOpen is what POST /api/projects answered: whether the path was added,
// was already known, is not a repo, or does not exist.
type ProjectOpen struct {
	Status  string   `json:"status"`
	Project *Project `json:"project"`
}

// ProjectRemoved is what POST /api/projects/remove answered, with the chats it
// moved back to the built-in workspace.
type ProjectRemoved struct {
	Status     string `json:"status"`
	MovedChats int    `json:"movedChats"`
}

// Project groups conversations that share a working directory.
type Project struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Path      string `json:"path"`
	Builtin   bool   `json:"builtin"`
	CreatedAt int64  `json:"createdAt"`
}

// Feishu is the bridge's health view of the Feishu transport.
type Feishu struct {
	Ready     bool   `json:"ready"`
	DownMs    int64  `json:"downMs"`
	Flaps     int    `json:"flaps"`
	LastError string `json:"lastError"`
}

// SearchHit is one match of GET /api/search, which looks inside transcript text
// across conversations rather than at titles.
type SearchHit struct {
	ConversationID int    `json:"conversationId"`
	EntryID        int    `json:"entryId"`
	Kind           string `json:"kind"`
	Snippet        string `json:"snippet"`
}

// State is GET /api/state.
type State struct {
	Surfaces []Surface `json:"surfaces"`
	Tasks    []Task    `json:"tasks"`
	Projects []Project `json:"projects"`
	Feishu   *Feishu   `json:"feishu"`
}

// LiveDoc is the "pi.live" document the socket pushes: the run in flight, its
// generation and the tool calls it has started.
type LiveDoc struct {
	Run        json.RawMessage   `json:"run"`
	Generation json.RawMessage   `json:"generation"`
	Tools      []json.RawMessage `json:"tools"`
}

// Frame is one socket message: a patch of the conversation the client asked
// about.
type Frame struct {
	Value *FrameValue `json:"value"`
}

// FrameValue carries the entries so far and the live documents by name.
type FrameValue struct {
	Entries []Entry            `json:"entries"`
	Docs    map[string]LiveDoc `json:"docs"`
}

// ModelRef identifies a model without its display data.
type ModelRef struct {
	Provider string `json:"provider"`
	ModelID  string `json:"modelId"`
}

// ModelEntry is one model the bridge can run.
type ModelEntry struct {
	Provider       string    `json:"provider"`
	ProviderName   string    `json:"providerName"`
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	Reasoning      bool      `json:"reasoning"`
	ContextWindow  int       `json:"contextWindow"`
	MaxTokens      int       `json:"maxTokens"`
	Cost           ModelCost `json:"cost"`
	ThinkingLevels []string  `json:"thinkingLevels"`
}

// ModelCost is the price per million tokens.
type ModelCost struct {
	Input  float64 `json:"input"`
	Output float64 `json:"output"`
}

// Usage is the running cost of the open conversation.
type Usage struct {
	Cost   float64 `json:"cost"`
	Tokens int     `json:"tokens"`
	Turns  int     `json:"turns"`
}

// Conversation is GET /api/conversation.
type Conversation struct {
	Entries []Entry `json:"entries"`
	// HasMore says the chat holds entries older than the ones returned, so the
	// transcript can offer to read further back.
	HasMore       bool      `json:"hasMore"`
	Model         *ModelRef `json:"model"`
	ThinkingLevel string    `json:"thinkingLevel"`
	Usage         *Usage    `json:"usage"`
}

// PendingApproval is one tool call waiting for the user's decision.
type PendingApproval struct {
	CallID    string `json:"callId"`
	GrantKey  string `json:"grantKey"`
	Tool      string `json:"tool"`
	Summary   string `json:"summary"`
	Reason    string `json:"reason"`
	SurfaceID string `json:"surfaceId"`
	// CreatedAt is epoch milliseconds: src/approvals.ts stamps it with
	// Date.now(), not seconds.
	CreatedAt int64 `json:"createdAt"`
}

// Note is one memory line.
type Note struct {
	At   string `json:"at"`
	Text string `json:"text"`
}

// Memory is GET /api/memory: the three scopes the client shows.
type Memory struct {
	Global []Note `json:"global"`
	Work   []Note `json:"work"`
	Chat   []Note `json:"chat"`
}

// FileChange is one entry of the worktree's change list.
type FileChange struct {
	Path    string `json:"path"`
	Status  string `json:"status"`
	Added   int    `json:"added"`
	Removed int    `json:"removed"`
}

// FilePayload is GET /api/file: a workspace file as the viewer shows it,
// truncated when it is too big to draw.
type FilePayload struct {
	Path      string `json:"path"`
	Content   string `json:"content"`
	Lines     int    `json:"lines"`
	Size      int    `json:"size"`
	Truncated bool   `json:"truncated"`
}

// Changes is GET /api/changes.
type Changes struct {
	Repo      bool         `json:"repo"`
	Branch    string       `json:"branch"`
	Diff      string       `json:"diff"`
	Files     []FileChange `json:"files"`
	Truncated bool         `json:"truncated"`
	Workspace string       `json:"workspace"`
	Error     string       `json:"error"`
}

// Commit is one commit in the branch view.
type Commit struct {
	Hash    string `json:"hash"`
	Short   string `json:"short"`
	Date    string `json:"date"`
	Subject string `json:"subject"`
	Files   int    `json:"files"`
	Added   int    `json:"added"`
	Removed int    `json:"removed"`
}

// Branch is one local branch with its tracking state.
type Branch struct {
	Name     string `json:"name"`
	Current  bool   `json:"current"`
	Upstream string `json:"upstream"`
	Track    string `json:"track"`
}

// Remote is where the worktree pushes to.
type Remote struct {
	URL    string `json:"url"`
	Kind   string `json:"kind"`
	WebURL string `json:"webUrl"`
}

// Git is GET /api/git.
type Git struct {
	Worktree   string   `json:"worktree"`
	Branch     string   `json:"branch"`
	Base       string   `json:"base"`
	Origin     string   `json:"origin"`
	Commits    []Commit `json:"commits"`
	Branches   []Branch `json:"branches"`
	Remote     Remote   `json:"remote"`
	CompareURL string   `json:"compareUrl"`
}

// PushResult is POST /api/push; Status names the variant, the rest are the
// fields that variant carries.
type PushResult struct {
	Status     string `json:"status"`
	Branch     string `json:"branch"`
	Remote     string `json:"remote"`
	CompareURL string `json:"compareUrl"`
	Output     string `json:"output"`
	Message    string `json:"message"`
}

// WorktreeRemoved is POST /api/worktree/remove: Status is "removed", or
// "unmerged" when the branch still holds commits that would be lost.
type WorktreeRemoved struct {
	Status  string `json:"status"`
	Branch  string `json:"branch"`
	Commits int    `json:"commits"`
	Message string `json:"message"`
}

// AccessLevel is how much a chat may do.
type AccessLevel string

const (
	AccessClosed AccessLevel = "closed"
	AccessOwner  AccessLevel = "owner"
	AccessGroup  AccessLevel = "group"
)

// AccessRow is one chat's access in the access panel.
type AccessRow struct {
	ChatID         string      `json:"chatId"`
	Level          AccessLevel `json:"level"`
	ConversationID int         `json:"conversationId"`
	Title          string      `json:"title"`
	ChatName       string      `json:"chatName"`
	ChatOwner      string      `json:"chatOwner"`
	Members        int         `json:"members"`
}

// AccessView is GET /api/access.
type AccessView struct {
	Chats     []AccessRow `json:"chats"`
	EnvChats  []string    `json:"envChats"`
	OwnerChat string      `json:"ownerChat"`
}

// AuditEvent is one row of the access audit log.
type AuditEvent struct {
	At     int64       `json:"at"`
	Kind   string      `json:"kind"`
	Who    string      `json:"who"`
	Chat   string      `json:"chat"`
	Level  AccessLevel `json:"level"`
	Tool   string      `json:"tool"`
	Detail string      `json:"detail"`
}

// TaskDetail is GET /api/task.
type TaskDetail struct {
	ID             int             `json:"id"`
	Kind           string          `json:"kind"`
	ConversationID int             `json:"conversationId"`
	Owner          *int            `json:"owner"`
	Background     bool            `json:"background"`
	AbortRequested bool            `json:"abortRequested"`
	Status         string          `json:"status"`
	Phase          *string         `json:"phase"`
	Until          *int64          `json:"until"`
	Title          string          `json:"title"`
	Input          json.RawMessage `json:"input"`
	Outcome        json.RawMessage `json:"outcome"`
	SubAgents      []TaskSubAgent  `json:"subAgents"`
}

// TaskSubAgent is one child conversation of a task: a build_feature pipeline's
// designer, implementer or tester, with how far it has got. A task with no
// children (an ordinary chat task) has none.
type TaskSubAgent struct {
	ConversationID int    `json:"conversationId"`
	Role           string `json:"role"`
	Steps          int    `json:"steps"`
	Tool           string `json:"tool"`
	Live           bool   `json:"live"`
	// StartedAt and EndedAt are wall clock milliseconds for the stage, so the
	// dialog can show how long this one has been running — the only progress a
	// pipeline that takes tens of minutes can honestly report. EndedAt is null
	// while the stage is still going.
	StartedAt *int64 `json:"startedAt"`
	EndedAt   *int64 `json:"endedAt"`
	// State is pending, live, done or failed. Rows draw from it rather than
	// from Live, because a stage that failed is not the same quiet as one that
	// finished.
	State string `json:"state"`
}

// MemorySuggestion is one candidate line from /api/memory/curate.
type MemorySuggestion struct {
	Text   string `json:"text"`
	Scope  string `json:"scope"`
	Reason string `json:"reason"`
}

// UsageDay is one cell of the usage calendar.
type UsageDay struct {
	Date   string  `json:"date"`
	Cost   float64 `json:"cost"`
	Tokens int     `json:"tokens"`
}
