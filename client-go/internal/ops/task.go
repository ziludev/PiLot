package ops

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"

	"pitta/internal/i18n"
	"pitta/internal/icons"
	"pitta/internal/protocol"
	"pitta/internal/theme"
)

// TaskProps is one task's detail: what it is doing, what it was asked, and the
// way out of it.
type TaskProps struct {
	Task     *protocol.TaskDetail
	Open     bool
	OnCancel func()
	OnClose  func()
}

// taskDialogWidth is wide enough for a spec line and narrow enough to leave the
// chat behind it visible.
const taskDialogWidth = 520

// backgroundLabel and requestLabel have no dictionary key: the client hardcodes
// them inline, so these English words stand in.
const (
	backgroundLabel = "background"
	requestLabel    = "Request"
)

// TaskDialog shows a task in a modal: its state, the request behind it, its
// result, and the button that cancels it.
func TaskDialog(c *ui.Context, p TaskProps) {
	if p.Task == nil || !p.Open {
		return
	}
	t := theme.Current(c)
	task := p.Task
	open := true
	// The panel closes itself through the button or the backdrop, and the app
	// hears about it once either way.
	notified := false
	notifyClose := func() {
		if notified {
			return
		}
		notified = true
		if p.OnClose != nil {
			p.OnClose()
		}
	}
	ui.Modal(c, &open, func() {
		ui.Column(c).Width(taskDialogWidth).Gap(10).Children(func() {
			ui.Row(c).FillWidth().Gap(7).AlignItems(ui.Center).Children(func() {
				ui.Box(c).Size(8, 8).Radius(4).Background(taskStatusColor(t, task.Status))
				ui.Text(c, task.Kind).FontSize(13).FontWeight(600).TextColor(t.Foreground).MaxLines(1)
				ui.Text(c, "#"+strconv.Itoa(task.ID)).FontSize(11).TextColor(t.MutedFg)
				ui.Spacer(c)
				badge(c, t, task.Status, t.Muted, t.MutedFg)
			})
			if meta := taskMeta(task); meta != "" {
				ui.Text(c, meta).FontSize(10).TextColor(t.MutedFg).Wrap()
			}
			ui.Column(c).FillWidth().Padding(8, 10).Radius(8).Background(t.Muted).
				Border(1, t.Border).Children(func() {
				text := task.Title
				if text == "" {
					text = untitledTask
				}
				ui.Text(c, text).FontSize(12).TextColor(t.Foreground).Wrap()
			})
			if asked := taskAsked(task.Input); asked != "" {
				caption(c, t, requestLabel)
				ui.Scroll(c).FillWidth().MaxHeight(140).Padding(6, 8).Radius(8).
					Background(t.Background).Border(1, t.Border).Children(func() {
					ui.Text(c, asked).FontSize(10).TextColor(t.MutedFg).Wrap()
				})
			}
			if outcome := prettyJSON(task.Outcome); outcome != "" {
				caption(c, t, i18n.T("logs"))
				ui.Scroll(c).FillWidth().MaxHeight(90).Padding(6, 8).Radius(8).
					Background(t.Background).Border(1, t.Border).Children(func() {
					ui.Text(c, outcome).FontSize(10).TextColor(t.MutedFg).Wrap()
				})
			}
			ui.Row(c).FillWidth().Gap(6).Justify(ui.End).Children(func() {
				label := i18n.T("cancel")
				if task.AbortRequested {
					label = cancelLabel
				}
				button(c, t, icons.Square, label, false, task.AbortRequested || task.Status == "terminal", p.OnCancel)
				button(c, t, icons.Check, closeLabel, true, false, func() {
					open = false
					notifyClose()
				})
			})
		})
	})
	if !open {
		notifyClose()
	}
}

// taskMeta is the state line under the title: where the task lives, what phase
// it is in, and when it wakes next.
func taskMeta(task *protocol.TaskDetail) string {
	parts := make([]string, 0, 4)
	if task.ConversationID != 0 {
		parts = append(parts, i18n.T("chat")+" #"+strconv.Itoa(task.ConversationID))
	}
	if task.Background {
		parts = append(parts, backgroundLabel)
	}
	if task.Phase != nil && *task.Phase != "" {
		parts = append(parts, *task.Phase)
	}
	if task.Until != nil {
		parts = append(parts, clock(*task.Until))
	}
	if task.AbortRequested {
		parts = append(parts, cancelLabel)
	}
	return strings.Join(parts, " · ")
}

// taskAsked is the request behind the task: a spec when the kind carries one, a
// plain text otherwise.
func taskAsked(input json.RawMessage) string {
	if len(input) == 0 {
		return ""
	}
	var spec struct {
		Spec string `json:"spec"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(input, &spec); err == nil {
		if spec.Spec != "" {
			return spec.Spec
		}
		if spec.Text != "" {
			return spec.Text
		}
	}
	return prettyJSON(input)
}

// prettyJSON indents a raw message for the log block; an empty message has no
// block at all.
func prettyJSON(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var out bytes.Buffer
	if err := json.Indent(&out, raw, "", "  "); err != nil {
		return string(raw)
	}
	return out.String()
}

// taskStatusColor marks a live task amber, a finished one quiet.
func taskStatusColor(t theme.Tokens, status string) ui.Color {
	switch status {
	case "running":
		return t.Warning
	case "terminal":
		return t.MutedFg
	}
	return t.Primary
}
