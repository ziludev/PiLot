// Package turns groups the agent transcript into turns and derives what a
// transcript view needs from each: steps, spend, files touched and phase time.
//
// Ported from the earlier web client (the old client/lib/turns.ts and
// the old client). The JSON field names read here are the ones those
// files read; comments name the payload they come from.
package turns

import (
	"encoding/json"
	"fmt"
	"log"
	"math"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf16"

	"pitta/internal/protocol"
)

// StepKind is what a step did: an answer, a tool call, or a failed tool call.
type StepKind int

const (
	StepThink StepKind = iota
	StepTool
	StepError
)

// Step is one assistant message on the turn's timeline. A tool call runs from
// its own entry to its result; an answer with no call is a think step.
type Step struct {
	ID     int
	Kind   StepKind
	Label  string
	Start  int64
	End    int64
	Entry  protocol.Entry
	Result *protocol.Entry
}

// ReportDoneTool is the call a turn ends on to say whether it met the goal it
// was given. It is the turn's answer rather than one of its tools, so it is
// not counted among them and is drawn as a verdict instead of a step.
const ReportDoneTool = "report_done"

// Phases is how the turn's wall time was spent, in milliseconds.
type Phases struct {
	Think int64
	Tool  int64
	Error int64
	Wait  int64
}

// Stats is the turn's derived accounting. Start, End and MS are nil when the
// turn has no timestamp at all, matching the optional TurnStats fields in the
// earlier web client; MS is nil until there are two timestamps to subtract.
type Stats struct {
	Tools   int
	Files   int
	Added   int
	Removed int
	Cost    float64
	Tokens  int
	Phases  Phases
	Start   *int64
	End     *int64
	MS      *int64
}

// Turn is one user message and everything the agent did in response.
type Turn struct {
	ID        int
	Anchor    protocol.Entry
	User      *protocol.Entry
	Finals    []protocol.Entry
	Reports   []protocol.Entry
	Notes     []protocol.Entry
	Internals []protocol.Entry
	Steps     []Step
	Stats     Stats
}

// GroupTurns splits entries into turns: a user message opens one, later
// assistant and tool entries belong to it, and app.note lines attach to the
// turn they were written in. Unknown non-pi kinds are kept, not dropped.
// waits are the conversation's approval waits, used for wait time.
func GroupTurns(entries []protocol.Entry, waits []protocol.ApprovalWait) []Turn {
	turns := make([]Turn, 0)
	current := -1

	for _, entry := range entries {
		// Bookkeeping kinds (pi.system, pi.compaction, ...) are not speech:
		// rendering them as an answer leaves an empty bubble.
		if isBookkeeping(entry.Kind) {
			continue
		}

		// A note belongs to the turn it was written in; with no turn yet it
		// opens one.
		if entry.Kind == "app.note" && current >= 0 {
			turns[current].Notes = append(turns[current].Notes, entry)
			continue
		}

		if entry.Kind == "pi.user" || entry.Kind == "app.feature-report" || current < 0 {
			turn := Turn{ID: entry.ID, Anchor: entry}
			if entry.Kind == "pi.user" {
				user := entry
				turn.User = &user
			}
			if entry.Kind == "app.feature-report" {
				turn.Reports = []protocol.Entry{entry}
			}
			if entry.Kind == "app.note" {
				turn.Notes = []protocol.Entry{entry}
			}
			turns = append(turns, turn)
			current = len(turns) - 1
			continue
		}

		if isThinkingOrTool(entry) {
			turns[current].Internals = append(turns[current].Internals, entry)
		} else {
			turns[current].Finals = append(turns[current].Finals, entry)
		}
	}

	for i := range turns {
		turns[i].derive(waits)
	}
	return turns
}

func isBookkeeping(kind string) bool {
	if !strings.HasPrefix(kind, "pi.") {
		return false
	}
	switch kind {
	case "pi.user", "pi.assistant", "pi.tool-result":
		return false
	}
	return true
}

// isThinkingOrTool keeps machinery out of the answer column: a tool-calling
// answer is machinery; a text answer is the answer.
func isThinkingOrTool(entry protocol.Entry) bool {
	if entry.Kind == "pi.tool-result" {
		return true
	}
	if entry.Kind != "pi.assistant" {
		return false
	}
	parts := partsOf(messages(entry.Model))
	hasCall := false
	for _, part := range parts {
		if part["type"] == "toolCall" {
			hasCall = true
			break
		}
	}
	hasText := strings.TrimSpace(textOf(parts)) != ""
	return hasCall || !hasText
}

func (t *Turn) derive(waits []protocol.ApprovalWait) {
	timestamps := make([]int64, 0, 1+len(t.Internals)+len(t.Finals))
	for _, entry := range append(append([]protocol.Entry{t.Anchor}, t.Internals...), t.Finals...) {
		if ts, ok := timestampOf(entry); ok {
			timestamps = append(timestamps, ts)
		}
	}

	timeline := make([]protocol.Entry, 0, len(t.Internals)+len(t.Finals))
	for _, entry := range t.Internals {
		if entry.Kind == "pi.assistant" {
			timeline = append(timeline, entry)
		}
	}
	for _, entry := range t.Finals {
		if entry.Kind == "pi.assistant" {
			timeline = append(timeline, entry)
		}
	}

	results := make(map[string]protocol.Entry)
	for _, entry := range t.Internals {
		if entry.Kind != "pi.tool-result" {
			continue
		}
		if id := toolCallID(entry); id != "" {
			results[id] = entry
		}
	}

	steps := make([]Step, 0, len(timeline))
	var phases Phases
	for index, entry := range timeline {
		parts := partsOf(messages(entry.Model))
		var call map[string]any
		for _, part := range parts {
			if part["type"] == "toolCall" {
				call = part
				break
			}
		}

		start := int64(0)
		if ts, ok := timestampOf(entry); ok {
			start = ts
		}

		var result *protocol.Entry
		if call != nil {
			id := ""
			if value := call["id"]; value != nil {
				id = fmt.Sprint(value)
			}
			if found, ok := results[id]; ok {
				entry := found
				result = &entry
			}
		}

		// The turn's own end is not known until every timestamp is collected
		// (the TS loop reads stats.end before assigning it), so a step with no
		// next entry ends where it started.
		end := start
		if result != nil {
			if ts, ok := timestampOf(*result); ok {
				end = ts
			}
		} else if index+1 < len(timeline) {
			if ts, ok := timestampOf(timeline[index+1]); ok {
				end = ts
			}
		}
		if end < start {
			end = start
		}

		kind := StepThink
		if call != nil {
			kind = StepTool
			if result != nil && resultIsError(*result) {
				kind = StepError
			}
		}
		steps = append(steps, Step{
			ID:     entry.ID,
			Kind:   kind,
			Label:  stepText(entry, call),
			Start:  start,
			End:    end,
			Entry:  entry,
			Result: result,
		})

		spent := end - start
		switch kind {
		case StepThink:
			phases.Think += spent
		case StepError:
			phases.Error += spent
		default:
			phases.Tool += spent
		}
	}

	paths := make(map[string]struct{})
	tools, added, removed := 0, 0, 0
	for _, entry := range t.Internals {
		for _, part := range partsOf(messages(entry.Model)) {
			switch part["type"] {
			case "toolCall":
				if name, _ := part["name"].(string); name == ReportDoneTool {
					continue // the verdict is the turn's answer, not one of its tools
				}
				tools++
				if args, ok := part["arguments"].(map[string]any); ok {
					if path, ok := args["path"].(string); ok {
						paths[path] = struct{}{}
					}
				}
			case "text":
				if text, ok := part["text"].(string); ok {
					if a, r, ok := diffStats(text); ok {
						added += a
						removed += r
					}
				}
			}
		}
	}

	// pi records usage on every assistant message; the turn's spend is their
	// sum.
	cost, tokens := 0.0, 0
	for _, entry := range t.Internals {
		addUsage(entry, &cost, &tokens)
	}
	for _, entry := range t.Finals {
		addUsage(entry, &cost, &tokens)
	}

	// Approval time inside this turn: a run that waited on a human should say
	// so.
	if len(timestamps) > 0 {
		turnStart, turnEnd := timestamps[0], timestamps[len(timestamps)-1]
		for _, wait := range waits {
			overlap := min(turnEnd, wait.End) - max(turnStart, wait.Start)
			if overlap > 0 {
				phases.Wait += overlap
			}
		}
	}

	stats := Stats{
		Tools:   tools,
		Files:   len(paths),
		Added:   added,
		Removed: removed,
		Cost:    cost,
		Tokens:  tokens,
		Phases:  phases,
	}
	if len(timestamps) > 0 {
		start, end := timestamps[0], timestamps[len(timestamps)-1]
		stats.Start = &start
		stats.End = &end
		if len(timestamps) >= 2 {
			ms := end - start
			stats.MS = &ms
		}
	}

	t.Steps = steps
	t.Stats = stats
}

func addUsage(entry protocol.Entry, cost *float64, tokens *int) {
	for _, message := range messages(entry.Model) {
		usage, ok := message["usage"].(map[string]any)
		if !ok {
			continue
		}
		if details, ok := usage["cost"].(map[string]any); ok {
			if total, ok := details["total"].(float64); ok {
				*cost += total
			}
		}
		if total, ok := usage["totalTokens"].(float64); ok {
			*tokens += int(total)
		}
	}
}

// messages decodes an entry's model. The wire keeps it raw so an unknown
// payload cannot break the decode; entries with no model (app.note) decode to
// nil.
func messages(raw json.RawMessage) []map[string]any {
	if len(raw) == 0 {
		return nil
	}
	var list []map[string]any
	if err := json.Unmarshal(raw, &list); err != nil {
		log.Printf("turns: model: %v", err)
		return nil
	}
	return list
}

// partsOf flattens message contents into parts, carrying the message's role
// and toolName onto each part the way the earlier web client's partsOf does.
func partsOf(list []map[string]any) []map[string]any {
	var parts []map[string]any
	for _, message := range list {
		switch content := message["content"].(type) {
		case string:
			parts = append(parts, map[string]any{
				"type": "text",
				"text": content,
				"role": message["role"],
			})
		case []any:
			for _, raw := range content {
				part, ok := raw.(map[string]any)
				if !ok {
					continue
				}
				copied := make(map[string]any, len(part)+2)
				for key, value := range part {
					copied[key] = value
				}
				copied["role"] = message["role"]
				copied["toolName"] = message["toolName"]
				parts = append(parts, copied)
			}
		}
	}
	return parts
}

func textOf(parts []map[string]any) string {
	var text strings.Builder
	for _, part := range parts {
		if part["type"] != "text" {
			continue
		}
		if value, ok := part["text"].(string); ok {
			text.WriteString(value)
		}
	}
	return text.String()
}

var whitespaceRun = regexp.MustCompile(`\s+`)

// stepText is the one readable line per step: "bash · pnpm test" or
// "思考 · 1.2k".
func stepText(entry protocol.Entry, call map[string]any) string {
	if call != nil {
		name := "tool"
		if value := call["name"]; value != nil {
			name = fmt.Sprint(value)
		}
		value := ""
		if args, ok := call["arguments"].(map[string]any); ok {
			for _, key := range []string{"command", "path", "query", "url", "pattern"} {
				if candidate, ok := args[key].(string); ok && candidate != "" {
					value = candidate
					break
				}
			}
		}
		if value == "" {
			return name
		}
		return name + " · " + truncate(whitespaceRun.ReplaceAllString(value, " "), 46)
	}

	var thinking strings.Builder
	for _, part := range partsOf(messages(entry.Model)) {
		if part["type"] != "thinking" {
			continue
		}
		if value, ok := part["thinking"].(string); ok {
			thinking.WriteString(value)
		}
	}
	if thinking.Len() == 0 {
		return "思考"
	}
	// JS counts UTF-16 units and toFixed rounds halves up; math.Round keeps
	// the same digits at the .05 boundary.
	units := utf16Length(thinking.String())
	thousands := math.Round(float64(units)/100) / 10
	return "思考 · " + strconv.FormatFloat(thousands, 'f', 1, 64) + "k"
}

// truncate cuts by UTF-16 code units, the unit JS's String.slice counts.
func truncate(text string, limit int) string {
	units := utf16.Encode([]rune(text))
	if len(units) <= limit {
		return text
	}
	return string(utf16.Decode(units[:limit]))
}

func utf16Length(text string) int {
	units := 0
	for _, r := range text {
		if r > 0xffff {
			units += 2
		} else {
			units++
		}
	}
	return units
}

func timestampOf(entry protocol.Entry) (int64, bool) {
	for _, message := range messages(entry.Model) {
		if value, ok := message["timestamp"].(float64); ok {
			return int64(value), true
		}
	}
	return 0, false
}

// toolCallID finds the result's call id. Real pi transcripts put toolCallId on
// the ToolResultMessage; older shapes put it on the part (the earlier web client only
// reads the part, the Rust port reads the message first).
func toolCallID(entry protocol.Entry) string {
	list := messages(entry.Model)
	for _, message := range list {
		if value, ok := message["toolCallId"].(string); ok && value != "" {
			return value
		}
	}
	for _, part := range partsOf(list) {
		if value, ok := part["toolCallId"].(string); ok && value != "" {
			return value
		}
	}
	return ""
}

// resultIsError reads isError, which pi-ai declares on ToolResultMessage; the
// part-level flag is the older shape.
func resultIsError(entry protocol.Entry) bool {
	list := messages(entry.Model)
	for _, message := range list {
		if value, ok := message["isError"].(bool); ok && value {
			return true
		}
	}
	for _, part := range partsOf(list) {
		if value, ok := part["isError"].(bool); ok && value {
			return true
		}
	}
	return false
}

// Clock renders a duration as the earlier web client does: 42ms, 3.7s, 42s, 1m30s,
// 2m.
func Clock(ms int64) string {
	switch {
	case ms < 1000:
		return strconv.FormatInt(ms, 10) + "ms"
	case ms < 60_000:
		if ms < 10_000 {
			tenths := math.Round(float64(ms)/100) / 10
			return strconv.FormatFloat(tenths, 'f', 1, 64) + "s"
		}
		return strconv.FormatFloat(math.Round(float64(ms)/1000), 'f', 0, 64) + "s"
	default:
		minutes := ms / 60_000
		seconds := int64(math.Round(float64(ms%60_000) / 1000))
		if seconds == 0 {
			return strconv.FormatInt(minutes, 10) + "m"
		}
		return strconv.FormatInt(minutes, 10) + "m" + strconv.FormatInt(seconds, 10) + "s"
	}
}

var hunkHeader = regexp.MustCompile(`^@@ -\d+(,\d+)? \+\d+(,\d+)? @@`)

// diffStats counts added and removed lines, and reports whether text is a
// unified diff at all. It is the earlier web client's diffStats: only lines starting
// with + or - count, and file headers are skipped first.
func diffStats(text string) (added, removed int, ok bool) {
	lines := strings.Split(text, "\n")
	isDiff := false
	for _, line := range lines {
		if strings.HasPrefix(line, "diff --git ") || hunkHeader.MatchString(line) {
			isDiff = true
			break
		}
	}
	if !isDiff {
		return 0, 0, false
	}

	first := -1
	for index, line := range lines {
		if strings.HasPrefix(line, "diff --git ") || strings.HasPrefix(line, "--- ") {
			first = index
			break
		}
	}
	body := lines
	if first > 0 {
		body = lines[first:]
	}

	for _, line := range body {
		if strings.HasPrefix(line, "--- ") ||
			strings.HasPrefix(line, "+++ ") ||
			strings.HasPrefix(line, "index ") ||
			strings.HasPrefix(line, "new file ") ||
			strings.HasPrefix(line, "deleted file ") {
			continue
		}
		if strings.HasPrefix(line, "+") {
			added++
		} else if strings.HasPrefix(line, "-") {
			removed++
		}
	}
	return added, removed, true
}
