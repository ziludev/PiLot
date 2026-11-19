package app

// Banners.
//
// The window can only say something while it has the user's eyes. A turn that
// ends in a chat they are not watching, and a write that stops to wait for
// their yes, are exactly the moments they have walked away — so the client asks
// the system to say it, and clicking the banner opens the chat it was about.
//
// What gets a banner is a change, not a state: the notifier remembers which
// chats had a turn running and which approvals have been seen, so a chat that
// finished five minutes ago does not ring on every poll.

import (
	"context"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo"

	"pitta/internal/i18n"
	"pitta/internal/protocol"
)

// alert is one banner: the chat that opens when it is clicked, the two lines it
// says, and the ID that lets a later banner about the same chat replace it
// rather than stack on top of it.
type alert struct {
	convID int
	title  string
	body   string
	id     string
	// ask marks an approval: somebody is waiting on the answer, so it is not
	// news the window may save for later.
	ask bool
}

// notifier reads each snapshot against the one before it. It holds only what a
// banner needs to tell a transition apart from a state: which chats were busy,
// and which approvals have already been announced.
type notifier struct {
	busy      map[int]bool
	approvals map[string]bool
	primed    bool
}

// observe diffs one snapshot against the last and returns the banners worth
// showing. watching says the user can currently see that chat — banners are for
// what they cannot. The first snapshot only primes the books: everything in it
// finished or started before the window looked, so none of it rings.
func (n *notifier) observe(surfaces []protocol.Surface, tasks []protocol.Task,
	pending []protocol.PendingApproval, watching func(int) bool) []alert {
	busy := map[int]bool{}
	for _, task := range tasks {
		if task.State == "running" {
			busy[task.ConversationID] = true
		}
	}
	if !n.primed {
		n.busy, n.approvals, n.primed = busy, map[string]bool{}, true
		for _, waiting := range pending {
			n.approvals[waiting.CallID] = true
		}
		return nil
	}

	stops := map[int]string{}
	for _, s := range surfaces {
		stops[s.ConversationID] = s.LastStop
	}
	var out []alert
	for _, s := range surfaces {
		conv := s.ConversationID
		if !n.busy[conv] || busy[conv] || watching(conv) {
			continue
		}
		out = append(out, alert{
			convID: conv,
			title:  titleOf(surfaces, conv),
			body:   stopLine(stops[conv]),
			id:     "chat-" + strconv.Itoa(conv),
		})
	}

	convOf := map[string]int{}
	for _, s := range surfaces {
		convOf[s.SurfaceID] = s.ConversationID
	}
	seen := map[string]bool{}
	for _, waiting := range pending {
		seen[waiting.CallID] = true
		if n.approvals[waiting.CallID] {
			continue
		}
		conv := convOf[waiting.SurfaceID]
		if watching(conv) {
			continue
		}
		out = append(out, alert{
			convID: conv,
			title:  titleOf(surfaces, conv),
			body:   i18n.TF("notifyApproval", map[string]any{"what": approvalWhat(waiting)}),
			id:     "ask-" + waiting.CallID,
			ask:    true,
		})
	}
	n.busy, n.approvals = busy, seen
	return out
}

// titleOf is the chat's name, or the one the rail would show when the bridge has
// not named it yet.
func titleOf(surfaces []protocol.Surface, conv int) string {
	for _, s := range surfaces {
		if s.ConversationID == conv && strings.TrimSpace(s.Title) != "" {
			return strings.TrimSpace(s.Title)
		}
	}
	return i18n.TF("chatNumber", map[string]any{"n": conv})
}

// stopLine is how the turn ended, in the words the banner has room for.
func stopLine(stop string) string {
	switch stop {
	case "error":
		return i18n.T("notifyError")
	case "abort":
		return i18n.T("notifyAbort")
	}
	return i18n.T("notifyDone")
}

// approvalWhat names the write the banner is about to the depth a notification
// body shows: the tool, and the summary when the bridge wrote one.
func approvalWhat(waiting protocol.PendingApproval) string {
	tool := strings.TrimSpace(waiting.Tool)
	summary := strings.TrimSpace(waiting.Summary)
	switch {
	case tool == "" && summary == "":
		return i18n.T("notifySomething")
	case summary == "":
		return tool
	case tool == "":
		return summary
	}
	return tool + " · " + summary
}

// delivery is how much the window may interrupt, and when. A finish is news: it
// waits for you in the rail's dot, so it can also wait for morning. An approval is
// a decision somebody filed and is standing next to, so it only respects the hours.
//
// The day's budget lives here, in this run, on purpose: a relaunch buys back five
// banners, which is the right size of amnesia for a counter whose job is to stop
// the window from becoming the thing you mute.
type delivery struct {
	from, to int // local hours, wrapping midnight; equal means never quiet
	budget   int // finished-turn banners a day; 0 means no ceiling
	day      string
	spent    int
	now      func() time.Time
}

// deliveryFor reads the two settings into a gate that speaks on behalf of them.
func deliveryFor(p prefs, now func() time.Time) delivery {
	from, to := p.quietWindow()
	return delivery{from: from, to: to, budget: p.bannerBudget(), now: now}
}

// at is the clock the gate reads. A window built without one (the test fixtures,
// which draw a frame at whatever hour the suite runs) is never quiet: a banner
// test that passes before eight and fails after ten would be measuring the clock.
func (d delivery) at() time.Time {
	if d.now == nil {
		return time.Time{}
	}
	return d.now()
}

// quiet says the hour is one the user asked the window to keep to itself.
func (d delivery) quiet(at time.Time) bool {
	if d.from == d.to {
		return false
	}
	hour := at.Hour()
	if d.from < d.to {
		return hour >= d.from && hour < d.to
	}
	return hour >= d.from || hour < d.to
}

// spend answers whether this banner is worth showing now, and books a finished
// turn against the day when it is. Approvals do not spend it: five finishes
// before lunch should not be able to silence the one decision blocking a task.
func (d *delivery) spend(al alert, at time.Time) bool {
	if at.IsZero() {
		return true
	}
	if d.quiet(at) {
		return false
	}
	if al.ask || d.budget <= 0 {
		return true
	}
	if day := at.Format("2006-01-02"); day != d.day {
		d.day, d.spent = day, 0
	}
	if d.spent >= d.budget {
		return false
	}
	d.spent++
	return true
}

// showBanner hands one alert to the system. It runs off the frame thread because
// Show waits for the answer — on first use that is the permission dialog, which
// is not something to block a paint on.
func (a *App) showBanner(al alert) {
	if a.prefs.Silent {
		return
	}
	note := mygo.NewNotification(mygo.NotificationOptions{
		ID:    al.id,
		Group: "chat-" + strconv.Itoa(al.convID),
		Title: al.title,
		Body:  al.body,
	})
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := note.ShowContext(ctx); err != nil && err != mygo.ErrNotificationsDenied {
			log.Printf("notify: %v", err)
		}
	}()
}

// NotificationClicked is what a click on any of this app's banners lands in,
// including one from an earlier run: the ID carries the chat, so the window only
// has to open it.
func (a *App) NotificationClicked(id string) {
	conv := 0
	if _, rest, ok := strings.Cut(id, "-"); ok {
		conv, _ = strconv.Atoi(rest)
	}
	if conv == 0 {
		if ask := a.askConversation(strings.TrimPrefix(id, "ask-")); ask != 0 {
			conv = ask
		}
	}
	if conv == 0 {
		return
	}
	a.post(event{kind: evSelect, convID: conv})
	if a.win != nil {
		a.win.Focus()
	}
}

// askConversation finds the chat an approval belongs to, for the banners that
// outlived the snapshot that announced them.
func (a *App) askConversation(callID string) int {
	for _, waiting := range a.pending {
		if waiting.CallID != callID {
			continue
		}
		for _, s := range a.surfaceSnapshot() {
			if s.SurfaceID == waiting.SurfaceID {
				return s.ConversationID
			}
		}
	}
	return 0
}

// watchingChat says the user has this chat in front of them: it is the open one
// and the window is the app they are looking at. A banner about a turn that ends
// while its own transcript is on screen would only interrupt the reading.
func (a *App) watchingChat(conv int) bool {
	if conv != a.active {
		return false
	}
	return a.win != nil && a.win.IsFocused()
}
