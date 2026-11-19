package ops

import (
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"

	"pitta/internal/i18n"
	"pitta/internal/icons"
	"pitta/internal/protocol"
	"pitta/internal/theme"
)

// AccessProps is the access tab: who may write in each chat, the owner chat and
// the env chats that stay closed.
type AccessProps struct {
	View     *protocol.AccessView
	Audit    []protocol.AuditEvent
	OnReload func()
}

// accessFilter is the segment the reader picked. It is package state for the
// same reason the memory folds are: Props carry no view state.
var (
	accessFilter  int
	accessFilters = []protocol.AccessLevel{"", protocol.AccessGroup, protocol.AccessOwner, protocol.AccessClosed}
)

// auditShown caps the audit list: the panel reports the newest decisions, the
// full history belongs to the log view.
const auditShown = 40

// AccessPanel draws the access tab: the level of every chat, where approvals
// go, the chats pinned shut by .env, and the decisions behind them.
func AccessPanel(c *ui.Context, p AccessProps) {
	t := theme.Current(c)
	ui.Column(c).FillWidth().Grow(1).Children(func() {
		heading(c, t, icons.ShieldCheck, i18n.T("access"), i18n.T("accessHint"), func() {
			glyph(c, t, icons.RotateCw, i18n.T("refresh"), p.OnReload)
		})
		ui.Row(c).FillWidth().Padding(0, 12, 8, 12).Children(func() {
			ui.Segmented(c, &accessFilter, levelLabelAll, levelLabelGroup, levelLabelOwner, levelLabelClosed).
				Label(i18n.T("access")).FillEqually().Height(22).FontSize(10)
		})
		ui.Scroll(c).FillWidth().Grow(1).Padding(0, 12).Children(func() {
			ui.Column(c).FillWidth().Gap(10).PaddingY(4).Children(func() {
				if p.View == nil {
					empty(c, t, readingLabel)
					return
				}
				accessTable(c, t, p.View)
				accessRouting(c, t, p.View)
				accessAudit(c, t, p.Audit)
			})
		})
	})
}

// accessTable is the chat list, narrowed to the picked level.
func accessTable(c *ui.Context, t theme.Tokens, view *protocol.AccessView) {
	want := protocol.AccessLevel("")
	if accessFilter >= 0 && accessFilter < len(accessFilters) {
		want = accessFilters[accessFilter]
	}
	rows := make([]protocol.AccessRow, 0, len(view.Chats))
	for _, row := range view.Chats {
		if want == "" || row.Level == want {
			rows = append(rows, row)
		}
	}

	ui.Column(c).FillWidth().Radius(theme.Radius).Background(t.Card).
		Border(1, t.Border).Children(func() {
		for i, row := range rows {
			if i > 0 {
				ui.Box(c).FillWidth().Height(1).Background(t.Border)
			}
			accessChatRow(c, t, view, row)
		}
		if len(rows) == 0 {
			empty(c, t, i18n.T("noHits"))
		}
	})
}

func accessChatRow(c *ui.Context, t theme.Tokens, view *protocol.AccessView, row protocol.AccessRow) {
	line := ui.Row(c).FillWidth().Padding(7, 10).Gap(8).AlignItems(ui.Center)
	// The owner chat is where approval cards land, so it is tinted like the
	// card that names it.
	owner := view.OwnerChat != "" && row.ChatID == view.OwnerChat
	if owner {
		line.Background(t.Accent.Mix(t.Primary, 0.10))
	}
	line.Children(func() {
		if owner {
			ui.Box(c).Width(2).Height(16).Radius(1).Background(t.Primary)
		}
		accessLevelBadge(c, t, row.Level)
		ui.Column(c).Grow(1).Gap(1).Children(func() {
			ui.Text(c, accessChatName(row)).FontSize(11).TextColor(t.Foreground).MaxLines(1)
			if meta := accessChatMeta(row); meta != "" {
				ui.Text(c, meta).FontSize(9).TextColor(t.MutedFg).MaxLines(1)
			}
		})
		if row.ConversationID != 0 {
			ui.Text(c, "#"+strconv.Itoa(row.ConversationID)).FontSize(9).TextColor(t.MutedFg)
		}
	})
}

// accessLevelBadge colors the level the way the client does: the owner's level
// is the primary tone, a group's is the accent, a closed chat stays muted.
func accessLevelBadge(c *ui.Context, t theme.Tokens, level protocol.AccessLevel) {
	switch level {
	case protocol.AccessOwner:
		badge(c, t, levelLabelOwner, t.Primary, t.PrimaryFg)
	case protocol.AccessGroup:
		badge(c, t, levelLabelGroup, t.Accent, t.AccentFg)
	default:
		badge(c, t, levelLabelClosed, t.Muted, t.MutedFg)
	}
}

func accessChatName(row protocol.AccessRow) string {
	if row.Title != "" {
		return row.Title
	}
	if row.ChatName != "" {
		return row.ChatName
	}
	return tail(row.ChatID)
}

func accessChatMeta(row protocol.AccessRow) string {
	parts := make([]string, 0, 2)
	if row.ChatOwner != "" {
		parts = append(parts, tail(row.ChatOwner))
	}
	if row.Members > 0 {
		parts = append(parts, strconv.Itoa(row.Members)+" "+membersLabel)
	}
	return strings.Join(parts, " · ")
}

// accessRouting names where approval cards go and which chats .env keeps shut;
// a chat listed there cannot be opened from this panel.
func accessRouting(c *ui.Context, t theme.Tokens, view *protocol.AccessView) {
	ui.Column(c).FillWidth().Padding(8, 10).Gap(5).Radius(theme.Radius).
		Background(t.Muted).Border(1, t.Border).Children(func() {
		ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
			caption(c, t, i18n.T("approvalsTo"))
			if view.OwnerChat != "" {
				ui.Text(c, tail(view.OwnerChat)).FontSize(10).TextColor(t.Foreground).MaxLines(1)
			} else {
				ui.Text(c, "—").FontSize(10).TextColor(t.MutedFg)
			}
		})
		ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
			caption(c, t, i18n.T("envChats"))
			if len(view.EnvChats) == 0 {
				ui.Text(c, "—").FontSize(10).TextColor(t.MutedFg)
			}
			for _, id := range view.EnvChats {
				ui.Text(c, tail(id)).FontSize(10).TextColor(t.Destructive).MaxLines(1)
			}
		})
		if len(view.EnvChats) > 0 {
			ui.Text(c, envLockedNote).FontSize(9).TextColor(t.Destructive).Wrap()
			ui.Text(c, i18n.T("envChatsHint")).FontSize(9).TextColor(t.MutedFg).Wrap()
		}
	})
}

// accessAudit is the tail of the decision log, newest first.
func accessAudit(c *ui.Context, t theme.Tokens, events []protocol.AuditEvent) {
	ui.Column(c).FillWidth().Gap(4).Children(func() {
		ui.Row(c).FillWidth().Gap(4).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, icons.ShieldCheck).Size(10, 10).TextColor(t.MutedFg)
			caption(c, t, i18n.T("auditRecent"))
		})
		if len(events) == 0 {
			ui.Column(c).FillWidth().Padding(10).Radius(6).Border(1, t.Border).Children(func() {
				ui.Text(c, i18n.T("auditEmpty")).FontSize(10).TextColor(t.MutedFg).Wrap()
			})
			return
		}
		if len(events) > auditShown {
			events = events[len(events)-auditShown:]
		}
		// The clock is a gutter, so it is printed when it changes and left
		// blank otherwise — five decisions in the same minute read as one
		// moment, not as five timestamps to compare.
		prev := ""
		for i := len(events) - 1; i >= 0; i-- {
			at := clock(events[i].At)
			auditRow(c, t, events[i], at != prev)
			prev = at
		}
	})
}

func auditRow(c *ui.Context, t theme.Tokens, event protocol.AuditEvent, showClock bool) {
	ui.Row(c).FillWidth().Padding(3, 4).Gap(6).AlignItems(ui.Start).Children(func() {
		stamp := ""
		if showClock {
			stamp = clock(event.At)
		}
		ui.Text(c, stamp).FontSize(9).TextColor(t.MutedFg).Width(72).MaxLines(1)
		auditKindBadge(c, t, event.Kind)
		ui.Text(c, auditDetail(event)).FontSize(10).TextColor(t.MutedFg).Wrap().Grow(1)
	})
}

func auditKindBadge(c *ui.Context, t theme.Tokens, kind string) {
	switch kind {
	case "deny":
		badge(c, t, kind, t.Destructive, t.PrimaryFg)
	case "approve":
		badge(c, t, kind, t.Primary, t.PrimaryFg)
	case "query":
		badge(c, t, kind, t.Accent, t.AccentFg)
	default:
		badge(c, t, kind, t.Muted, t.MutedFg)
	}
}

// auditDetail joins the tool and the note the way the client does; either half
// may be empty.
func auditDetail(event protocol.AuditEvent) string {
	if event.Tool != "" && event.Detail != "" {
		return event.Tool + " · " + event.Detail
	}
	if event.Tool != "" {
		return event.Tool
	}
	return event.Detail
}
