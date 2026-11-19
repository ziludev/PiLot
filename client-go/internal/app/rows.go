package app

import (
	"encoding/json"
	"sort"
	"time"

	"github.com/egoist/mygo/ui"

	"pitta/internal/i18n"
	"pitta/internal/protocol"
)

// The projections: railRows() and threadRows(convID) turn the mirrored bridge
// state into the rows a frame draws. They are pure functions of the state they
// are handed, so a cache keyed by its version is all the invalidation there is.

type railSection int

const (
	railChats railSection = iota
	railTasks
)

type railRowKind int

const (
	railChat railRowKind = iota
	railTask
	railGroup
	railHint
)

type railRow struct {
	kind railRowKind
	conv protocol.Surface
	task protocol.Task

	// label is a group's title or a hint line; when is a task's next fire.
	label string
	when  string
	// cost is what the task's last run cost, drawn under its next fire.
	cost string
	// meta is the chat's source, empty for chats that have none worth naming.
	meta string
	// at is the chat's newest activity, drawn as a relative time when known.
	at     time.Time
	unread bool
}

// sourceLabel names where a chat came from, in the rail's own words.
func sourceLabel(kind string) string {
	switch kind {
	case "feishu":
		return i18n.T("sourceFeishu")
	case "client":
		return i18n.T("sourceClient")
	case "other":
		return i18n.T("sourceOther")
	}
	return ""
}

// railRows projects the rail's list: the filtered chats, or the tasks by group.
func (a *App) railRows() []railRow {
	rows := make([]railRow, 0, len(a.surfaces)+8)
	if a.rail == railChats {
		filter := lowerTrim(a.filter)
		for _, s := range a.sortedSurfaces() {
			if s.Archived {
				continue
			}
			if filter != "" && !containsFold(s.Title, filter) {
				continue
			}
			at := a.vm.tails[s.ConversationID]
			rows = append(rows, railRow{
				kind:   railChat,
				conv:   s,
				meta:   sourceLabel(s.Kind),
				at:     at,
				unread: a.vm.unread[s.ConversationID] && s.ConversationID != a.active,
			})
		}
		if len(rows) == 0 {
			rows = append(rows, railRow{kind: railHint, label: emptyChatsLabel(filter)})
		}
		return rows
	}

	return a.taskRows(lowerTrim(a.filter))
}

// sortedSurfaces puts the chat that last spoke first, and falls back to the
// newest conversation when no tail has come back yet.
func (a *App) sortedSurfaces() []protocol.Surface {
	surfaces := a.surfaceSnapshot()
	sort.SliceStable(surfaces, func(i, j int) bool {
		left, right := a.vm.tails[surfaces[i].ConversationID], a.vm.tails[surfaces[j].ConversationID]
		if !left.Equal(right) {
			return left.After(right)
		}
		return surfaces[i].ConversationID > surfaces[j].ConversationID
	})
	return surfaces
}

func emptyChatsLabel(filter string) string {
	if filter != "" {
		return i18n.T("noMatches")
	}
	return i18n.T("noChats")
}

type threadRowKind int

const (
	rowUser threadRowKind = iota
	rowAssistant
	rowSteps
	rowError
	rowLive
)

type step struct {
	label string
	body  string
	at    time.Time
}

type imageRef struct {
	mime string
	data []byte

	// decoded and cache keep a thumbnail's bitmap for the frames after the
	// first: decoding a screenshot is not frame work.
	decoded bool
	cache   *ui.Bitmap
}

type threadRow struct {
	kind threadRowKind

	// id is stable across frames, so a folded step row keeps its state.
	id     string
	text   string
	images []imageRef

	// steps is the fold of one turn's thinking and tool calls; turn and the
	// chat it belongs to address the disclosure state.
	steps []step
	turn  int

	// liveStep is the tool the turn in flight is running now, for rowLive.
	liveStep string
	span     time.Duration
	noSpan   bool
}

// foldKey addresses one turn's disclosure state.
func foldKey(conversationID, turnID int) string {
	return itoa(conversationID) + ":" + itoa(turnID)
}

// threadRows projects one chat's entries into the transcript's rows: the user's
// bubble, the assistant's text, one folded step row per run, and errors.
func (a *App) threadRows(conversationID int) []threadRow {
	th := a.threads[conversationID]
	if th == nil || !th.loaded {
		return nil
	}
	cache, ok := a.vm.projection[conversationID]
	if ok && cache.version == th.version {
		return cache.rows
	}
	rows, turns := projectEntries(conversationID, th.entries)
	a.vm.projection[conversationID] = projectionCache{version: th.version, rows: rows, turns: turns}
	return rows
}

type projectionCache struct {
	version uint64
	rows    []threadRow
	turns   []int
}

// projectEntries is the pure half of threadRows: entries in, rows out.
func projectEntries(conversationID int, entries []protocol.Entry) ([]threadRow, []int) {
	ordered := make([]protocol.Entry, len(entries))
	copy(ordered, entries)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].ID < ordered[j].ID })

	rows := make([]threadRow, 0, len(ordered))
	var turns []int
	turn := 0
	stepRows := map[int]int{} // turn id → index in rows

	for _, entry := range ordered {
		view := parseEntry(entry)
		if view.userText != "" || len(view.images) > 0 {
			turn = entry.ID
			rows = append(rows, threadRow{
				kind:   rowUser,
				id:     "u" + itoa(entry.ID),
				text:   view.userText,
				images: view.images,
			})
		}
		for _, block := range view.blocks {
			switch block.kind {
			case "text":
				rows = append(rows, threadRow{
					kind:   rowAssistant,
					id:     "a" + itoa(entry.ID) + "." + itoa(block.index),
					text:   block.text,
					images: block.images,
				})
			case "tool-result":
				// A result belongs to the call above it: one line per step.
				if turn == 0 {
					turn = entry.ID
				}
				if idx, ok := stepRows[turn]; ok && len(rows[idx].steps) > 0 {
					last := &rows[idx].steps[len(rows[idx].steps)-1]
					last.body = joinSummary(last.body, block.text)
					if !block.at.IsZero() {
						last.at = block.at
					}
					continue
				}
				fallthrough
			default: // step
				if turn == 0 {
					turn = entry.ID
				}
				step := step{label: block.name, body: block.text, at: block.at}
				if step.label == "" {
					step.label = i18n.T("thinking")
				}
				idx, ok := stepRows[turn]
				if !ok {
					rows = append(rows, threadRow{
						kind: rowSteps,
						id:   foldKey(conversationID, turn),
						turn: turn,
					})
					idx = len(rows) - 1
					stepRows[turn] = idx
					turns = append(turns, turn)
				}
				rows[idx].steps = append(rows[idx].steps, step)
			}
		}
		if view.errText != "" {
			rows = append(rows, threadRow{
				kind: rowError,
				id:   "e" + itoa(entry.ID),
				text: view.errText,
				// Which words to send again when the reader asks to retry.
				turn: turn,
			})
		}
	}
	for _, idx := range stepRows {
		span, ok := stepsSpan(rows[idx].steps)
		rows[idx].span = span
		rows[idx].noSpan = !ok
	}
	return rows, turns
}

// stepsSpan is the time the folded steps took: first step to last.
func stepsSpan(steps []step) (time.Duration, bool) {
	var first, last time.Time
	for _, s := range steps {
		if s.at.IsZero() {
			continue
		}
		if first.IsZero() {
			first = s.at
		}
		last = s.at
	}
	if first.IsZero() || last.IsZero() || last.Before(first) {
		return 0, false
	}
	return last.Sub(first), true
}

// joinSummary adds a tool's result to the line that names the call.
func joinSummary(body, result string) string {
	result = clip(oneLine(result), 80)
	switch {
	case body == "":
		return result
	case result == "":
		return body
	}
	return body + " → " + result
}

// entryBlock is one drawable piece of an entry, in source order.
type entryBlock struct {
	kind   string // text, step, tool-result
	index  int
	text   string
	name   string
	at     time.Time
	images []imageRef
}

type entryView struct {
	userText string
	images   []imageRef
	blocks   []entryBlock
	errText  string
}

// parseEntry reads one bridge entry into the pieces the transcript draws. The
// entry's model is the pi message that produced it, sometimes several.
func parseEntry(entry protocol.Entry) entryView {
	var view entryView
	if len(entry.Model) == 0 {
		return view
	}
	var messages []modelMessage
	if err := json.Unmarshal(entry.Model, &messages); err != nil {
		return view
	}
	blockIndex := 0
	for _, msg := range messages {
		at := unixTime(msg.Timestamp)
		if msg.Role == "user" {
			text, images := userContent(msg.Content)
			view.userText = text
			view.images = images
		}
		if msg.Role == "toolResult" {
			view.blocks = append(view.blocks, entryBlock{
				kind:   "tool-result",
				text:   contentText(msg.Content),
				images: contentImages(msg.Content),
				at:     at,
			})
		}
		for _, part := range modelParts(msg.Content) {
			switch part.Type {
			case "text":
				if msg.Role == "assistant" && part.Text != "" {
					view.blocks = append(view.blocks, entryBlock{kind: "text", index: blockIndex, text: part.Text, at: at})
					blockIndex++
				}
			case "thinking":
				view.blocks = append(view.blocks, entryBlock{kind: "step", name: i18n.T("thinking"), text: oneLine(part.Thinking), at: at})
			case "toolCall":
				view.blocks = append(view.blocks, entryBlock{kind: "step", name: part.Name, text: argsSummary(part.Arguments), at: at})
			case "image":
				// A user's pictures belong to their bubble; one that comes back
				// inside an assistant message is drawn with the answer.
				if msg.Role != "user" {
					view.blocks = append(view.blocks, entryBlock{kind: "text", index: blockIndex, images: imageRefs(part), at: at})
					blockIndex++
				}
			}
		}
		if msg.ErrorMessage != "" || msg.StopReason == "error" {
			text := msg.ErrorMessage
			if text == "" {
				text = i18n.T("loadFailed")
			}
			view.errText = text
		}
	}
	return view
}

type modelMessage struct {
	Role         string          `json:"role"`
	Content      json.RawMessage `json:"content"`
	Timestamp    int64           `json:"timestamp"`
	StopReason   string          `json:"stopReason"`
	ErrorMessage string          `json:"errorMessage"`
}

type modelPart struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	Thinking  string          `json:"thinking"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
	Data      string          `json:"data"`
	MimeType  string          `json:"mimeType"`
}

// modelParts reads a message's content, which is a string for plain text and a
// list of parts for everything else.
func modelParts(content json.RawMessage) []modelPart {
	trimmed := trimSpaceBytes(content)
	if len(trimmed) == 0 || trimmed[0] != '[' {
		return nil
	}
	var parts []modelPart
	if err := json.Unmarshal(trimmed, &parts); err != nil {
		return nil
	}
	return parts
}

func userContent(content json.RawMessage) (string, []imageRef) {
	parts := modelParts(content)
	if parts == nil {
		return contentText(content), nil
	}
	var texts []string
	var images []imageRef
	for _, part := range parts {
		switch part.Type {
		case "text":
			if part.Text != "" {
				texts = append(texts, part.Text)
			}
		case "image":
			images = append(images, imageRefs(part)...)
		}
	}
	return joinLines(texts), images
}

func contentText(content json.RawMessage) string {
	var text string
	if err := json.Unmarshal(content, &text); err == nil {
		return text
	}
	parts := modelParts(content)
	var texts []string
	for _, part := range parts {
		if part.Type == "text" && part.Text != "" {
			texts = append(texts, part.Text)
		}
	}
	return joinLines(texts)
}

func contentImages(content json.RawMessage) []imageRef {
	parts := modelParts(content)
	var images []imageRef
	for _, part := range parts {
		if part.Type == "image" {
			images = append(images, imageRefs(part)...)
		}
	}
	return images
}

func imageRefs(part modelPart) []imageRef {
	data := decodeBase64(part.Data)
	if len(data) == 0 {
		return nil
	}
	mime := part.MimeType
	if mime == "" {
		mime = "image/png"
	}
	return []imageRef{{mime: mime, data: data}}
}

// argsSummary is a tool call's arguments on one line, for the folded step.
func argsSummary(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var object map[string]any
	if err := json.Unmarshal(raw, &object); err == nil {
		for _, key := range []string{"path", "command", "query", "url", "file"} {
			if value, ok := object[key]; ok {
				if text, ok := value.(string); ok && text != "" {
					return clip(oneLine(text), 80)
				}
			}
		}
	}
	return clip(oneLine(string(raw)), 80)
}
