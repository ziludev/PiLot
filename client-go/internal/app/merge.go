package app

// 合并请求: the third tab of the changes drawer. It reads one thing — the open
// merge request whose source is this chat's branch — and offers the two writes
// that follow from it: open one, and merge it. Both go through the bridge, which
// hands glab the arguments; the window never holds a token.

import (
	"context"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"pitta/internal/i18n"
	"pitta/internal/icons"
	"pitta/internal/protocol"
	"pitta/internal/theme"
)

// refreshMerge reads the request for the conversation now on screen.
func (a *App) refreshMerge() {
	if a.changes.conv == 0 {
		return
	}
	conv := a.changes.conv
	a.changes.mrBusy = true
	a.changes.mrConv = conv
	a.invalidate()
	go func() {
		ctx, cancel := context.WithTimeout(a.ctx, 45*time.Second)
		defer cancel()
		out, err := a.api.MergeRequestOf(ctx, conv)
		a.post(event{kind: evMerge, convID: conv, merge: &out, err: err})
	}()
}

// applyMerge takes the read's answer only for the chat it was read for: the
// drawer may have been reopened on another one while glab was thinking.
func (a *App) applyMerge(conv int, out *protocol.MRStatus, err error) {
	if conv != a.changes.mrConv {
		return
	}
	a.changes.mrBusy = false
	if err != nil {
		a.changes.mrFailed = err.Error()
		a.invalidate()
		return
	}
	a.changes.mrFailed = ""
	if out != nil {
		a.changes.mr = *out
		// The branch and the base are the same facts the other tabs show, and the
		// merge tab is the one that reads them from GitLab's answer.
		if out.Branch != "" {
			a.changes.branch = out.Branch
		}
		if out.Base != "" {
			a.changes.base = out.Base
		}
	}
	a.invalidate()
}

// createMR opens a request for the branch. The title starts as the branch name,
// which is at least honest, and the drawer lets it be edited first.
func (a *App) createMR() {
	if a.changes.conv == 0 || a.changes.mrBusy {
		return
	}
	conv := a.changes.conv
	title := strings.TrimSpace(a.changes.mrTitle)
	if title == "" {
		title = a.changes.branch
	}
	draft := a.changes.mrDraftTitle
	a.changes.mrBusy = true
	a.invalidate()
	go func() {
		ctx, cancel := context.WithTimeout(a.ctx, 120*time.Second)
		defer cancel()
		out, err := a.api.CreateMergeRequest(ctx, conv, title, "", draft)
		a.post(event{kind: evMergeWrite, convID: conv, mergeWrite: &out, err: err})
	}()
}

// mergeMR merges the open request. The query is not a confirmation dialog: the
// button that reaches it says 合并 and sits on the card being merged.
func (a *App) mergeMR(iid int) {
	if a.changes.conv == 0 || a.changes.mrBusy {
		return
	}
	conv := a.changes.conv
	squash := a.changes.mrSquash
	a.changes.mrBusy = true
	a.invalidate()
	go func() {
		ctx, cancel := context.WithTimeout(a.ctx, 180*time.Second)
		defer cancel()
		out, err := a.api.MergeMergeRequest(ctx, conv, iid, squash, true)
		a.post(event{kind: evMergeWrite, convID: conv, mergeWrite: &out, err: err})
	}()
}

func (a *App) applyMergeWrite(conv int, out *protocol.MRWrite, err error) {
	if conv != a.changes.mrConv {
		return
	}
	a.changes.mrBusy = false
	if err != nil {
		a.changes.mrFailed = err.Error()
		a.invalidate()
		return
	}
	a.changes.mrFailed = ""
	if out == nil {
		a.invalidate()
		return
	}
	switch out.Status {
	case "created":
		a.changes.toast = i18n.T("mrCreated")
	case "merged":
		a.changes.toast = i18n.T("mrMerged")
	case "":
	default:
		a.changes.mrFailed = a.mergeReasonText(out.Status, out.Message)
	}
	a.refreshMerge()
	a.invalidate()
}

// mergeReasonText turns a status into a sentence, and keeps what the CLI said
// when it said something: the sentence alone cannot tell a missing token from a
// rejected one.
func (a *App) mergeReasonText(status, message string) string {
	keys := map[string]string{
		"not-authenticated": "mrNeedLogin",
		"no-glab":           "mrNoGlab",
		"no-remote":         "mrNoRemote",
		"not-gitlab":        "mrNotGitlab",
		"detached":          "mrDetached",
		"failed":            "mrFailed",
	}
	key, ok := keys[status]
	if !ok {
		return message
	}
	text := i18n.T(key)
	if message != "" {
		text = text + " · " + message
	}
	return text
}

// mergeBody is the tab.
func (a *App) mergeBody(c *ui.Context, t theme.Tokens) {
	ui.Column(c).FillWidth().Grow(1).Padding(4, 10).Gap(10).Children(func() {
		if a.changes.mrFailed != "" {
			ui.Text(c, a.changes.mrFailed).FontSize(theme.TextSmall).TextColor(t.Red).
				Padding(4, 0).MaxLines(4)
		}
		if a.changes.mrBusy {
			ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
				ui.Spinner(c).Size(12, 12).TextColor(t.Ink3)
				ui.Text(c, i18n.T("loading")).FontSize(theme.TextSmall).TextColor(t.Ink3)
			})
		}
		status := a.changes.mr
		switch {
		case a.changes.conv == 0:
			ui.Text(c, i18n.T("mrNeedBranch")).FontSize(theme.TextSmall).TextColor(t.Ink3)
			return
		case status.Status == "":
			// Nothing has come back yet; the spinner above is the whole answer.
			return
		case status.Status != "ok":
			a.mergeUnavailable(c, t, status)
			return
		case status.MR == nil:
			a.mergeCreateForm(c, t)
		default:
			a.mergeCard(c, t, status.MR)
		}
	})
}

// mergeUnavailable says which part is missing, because the fix differs: a remote,
// a login, a binary.
func (a *App) mergeUnavailable(c *ui.Context, t theme.Tokens, status protocol.MRStatus) {
	text := a.mergeReasonText(status.Status, status.Message)
	ui.Text(c, text).FontSize(theme.TextSmall).TextColor(t.Ink3).MaxLines(4)
	switch status.Status {
	case "not-authenticated":
		ui.Text(c, "glab auth login").Font(monoFont).FontSize(theme.TextSmall).TextColor(t.Ink2)
	case "no-remote":
		ui.Text(c, i18n.T("mrNoRemoteTip")).FontSize(theme.TextMicro).TextColor(t.Ink3).MaxLines(3)
	}
}

// mergeCreateForm is what the tab shows before a request exists: a title to fix,
// and the two flags that change what gets opened.
func (a *App) mergeCreateForm(c *ui.Context, t theme.Tokens) {
	ui.Text(c, i18n.T("mrNone")).FontSize(theme.TextSmall).TextColor(t.Ink3)
	ui.Row(c).FillWidth().Height(32).Padding(0, 8).Radius(theme.RadiusControl).
		Background(t.Field).Border(1, t.LineSoft).AlignItems(ui.Center).Children(func() {
		field := ui.TextInputBase(c, &a.changes.mrTitle).FillWidth().Height(24).
			FontSize(theme.TextSmall).Placeholder(i18n.T("mrTitle")).TextColor(t.Ink)
		field.OnSubmit(func() { a.createMR() })
	})
	ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
		a.mergeFlag(c, t, "mrDraft", &a.changes.mrDraftTitle)
		ui.Spacer(c)
		a.mergeAction(c, t, i18n.T("mrCreate"), func() { a.createMR() })
	})
	ui.Text(c, i18n.TF("mrFrom", map[string]any{"branch": a.changes.branch, "base": a.changes.base})).
		FontSize(theme.TextMicro).TextColor(t.Ink3).MaxLines(2)
}

// mergeCard is an open request: what it is, whether it would merge, and the two
// ways out of here.
// openExternal is the browser opener, a variable so a test can watch the URL
// instead of launching a tab.
var openExternal = mygo.Shell.OpenExternal

func (a *App) mergeCard(c *ui.Context, t theme.Tokens, mr *protocol.MergeRequest) {
	ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
		ui.Text(c, "!"+itoa(mr.IID)).Font(monoFont).FontSize(theme.TextSmall).TextColor(t.Ink3)
		ui.Text(c, mr.State).FontSize(theme.TextMicro).TextColor(t.Ink3)
		if mr.Draft {
			ui.Text(c, i18n.T("mrDraft")).FontSize(theme.TextMicro).FontWeight(600).TextColor(t.Orange)
		}
	})
	ui.Text(c, mr.Title).FontSize(theme.TextBody).FontWeight(500).TextColor(t.Ink).MaxLines(3)

	ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
		ui.Text(c, mr.Source).Font(monoFont).FontSize(theme.TextMicro).TextColor(t.Ink2).MaxLines(1)
		ui.Text(c, "→").FontSize(theme.TextMicro).TextColor(t.Ink3)
		ui.Text(c, mr.Target).Font(monoFont).FontSize(theme.TextMicro).TextColor(t.Ink2).MaxLines(1)
	})
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		label, color := i18n.T("mrUnchecked"), t.Ink3
		switch mr.Merge {
		case "mergeable":
			label, color = i18n.T("mrMergeable"), t.Green
		case "conflicted":
			label, color = i18n.T("mrConflicted"), t.Red
		}
		ui.Text(c, label).FontSize(theme.TextMicro).FontWeight(500).TextColor(color)
		if mr.Notes > 0 {
			ui.Text(c, i18n.TF("mrNotes", map[string]any{"n": mr.Notes})).
				FontSize(theme.TextMicro).TextColor(t.Ink3)
		}
		if mr.Author != "" {
			ui.Text(c, mr.Author).FontSize(theme.TextMicro).TextColor(t.Ink3).MaxLines(1)
		}
	})

	ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
		if mr.WebURL != "" {
			url := mr.WebURL
			a.mergeAction(c, t, i18n.T("mrOpenPage"), func() { openExternal(url) })
		}
		ui.Spacer(c)
		a.mergeFlag(c, t, "mrSquash", &a.changes.mrSquash)
		iid := mr.IID
		a.mergeAction(c, t, i18n.T("mrMerge"), func() { a.mergeMR(iid) })
	})
}

// mergeFlag is a checkbox the width of its label: a small box, then the word.
func (a *App) mergeFlag(c *ui.Context, t theme.Tokens, key string, on *bool) {
	ui.ButtonBase(c).Height(22).Radius(theme.RadiusChip).Padding(0, 6).Gap(5).
		AlignItems(ui.Center).Cursor(ui.CursorPointer).OnClick(func() {
		*on = !*on
		a.invalidate()
	}).Children(func() {
		box := ui.Row(c).Size(12, 12).Radius(3).AlignItems(ui.Center).Justify(ui.Center)
		if *on {
			box = box.Background(t.Ink)
			box.Children(func() { ui.Icon(c, icons.Check).Size(9, 9).TextColor(t.Surface) })
		} else {
			box = box.Border(1, t.LineStrong)
		}
		ui.Text(c, i18n.T(key)).FontSize(theme.TextMicro).TextColor(t.Ink2)
	})
}

// mergeAction is the button pair the tab uses: filled for the one that writes,
// outlined for the one that only opens a page.
func (a *App) mergeAction(c *ui.Context, t theme.Tokens, label string, click func()) {
	primary := label == i18n.T("mrMerge") || label == i18n.T("mrCreate")
	a.mergeButton(c, t, label, primary, click)
}

// mergeSecondaryAction is the outlined one: it opens a page or moves nothing.
func (a *App) mergeSecondaryAction(c *ui.Context, t theme.Tokens, label string, click func()) {
	a.mergeButton(c, t, label, false, click)
}

func (a *App) mergeButton(c *ui.Context, t theme.Tokens, label string, primary bool, click func()) {
	btn := ui.ButtonBase(c).Height(24).Radius(theme.RadiusControl).Padding(0, 10).
		AlignItems(ui.Center).Justify(ui.Center).Cursor(ui.CursorPointer).OnClick(click)
	if primary {
		btn = btn.Background(t.Brand)
		btn.Children(func() {
			ui.Text(c, label).FontSize(theme.TextMicro).FontWeight(500).TextColor(ui.RGBA(255, 255, 255, 1))
		})
	} else {
		btn = btn.Background(t.Field).Border(1, t.LineSoft)
		btn.Children(func() { ui.Text(c, label).FontSize(theme.TextMicro).FontWeight(500).TextColor(t.Ink2) })
	}
}
