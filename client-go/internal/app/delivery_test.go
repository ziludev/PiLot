package app

import (
	"testing"
	"time"

	"pilot/internal/i18n"
	"pilot/internal/protocol"
)

// The window is allowed to interrupt, but not all night and not all day. These
// are the two numbers from the settings file, and the rules that read them: a
// finish is news and can wait for the rail's dot, an approval is a decision
// somebody is standing next to and only respects the hours.

func at(hour, minute int) time.Time {
	return time.Date(2026, 11, 20, hour, minute, 0, 0, time.Local)
}

func gate(quiet string, cap string, hour, minute int) delivery {
	now := at(hour, minute)
	return deliveryFor(prefs{Quiet: quiet, DailyCap: cap}, func() time.Time { return now })
}

func finished() alert { return alert{convID: 12, id: "chat-12"} }
func asking() alert   { return alert{convID: 12, id: "ask-c1", ask: true} }

func TestQuietHoursHoldAFinishedTurnUntilMorning(t *testing.T) {
	night := gate("", "", 23, 40)
	if night.spend(finished(), night.at()) {
		t.Fatalf("rang at 23:40 with the default quiet hours")
	}
	morning := gate("", "", 8, 5)
	if !morning.spend(finished(), morning.at()) {
		t.Fatalf("stayed quiet at 08:05, past the window")
	}
}

func TestTheQuietWindowWrapsMidnight(t *testing.T) {
	for _, tc := range []struct {
		hour int
		want bool
	}{{22, true}, {23, true}, {0, true}, {7, true}, {8, false}, {15, false}, {21, false}} {
		g := gate("", "", tc.hour, 0)
		if got := g.quiet(g.at()); got != tc.want {
			t.Fatalf("%02d:00 quiet = %v, want %v", tc.hour, got, tc.want)
		}
	}
}

func TestTheDayOnlySpendsFiveFinishedTurns(t *testing.T) {
	g := gate("", "", 10, 0)
	for i := 0; i < 5; i++ {
		if !g.spend(finished(), g.at()) {
			t.Fatalf("banner %d was held back inside the budget", i+1)
		}
	}
	if g.spend(finished(), g.at()) {
		t.Fatalf("the sixth finished turn rang; the day was already spent")
	}
	// Tomorrow is a new day, and the counter knows it.
	tomorrow := at(10, 0).Add(24 * time.Hour)
	if !g.spend(finished(), tomorrow) {
		t.Fatalf("the counter did not reset on the next day")
	}
}

// A morning of finished turns must not be able to silence the one decision that
// is blocking somebody: the budget buys quiet, not a missed approval.
func TestAnApprovalOutlivesTheBannerBudget(t *testing.T) {
	g := gate("", "", 10, 0)
	for i := 0; i < 8; i++ {
		g.spend(finished(), g.at())
	}
	if !g.spend(asking(), g.at()) {
		t.Fatalf("the approval was held back by spent banners")
	}
}

// Neither setting can be turned up so far that the window speaks at 3am: quiet
// hours apply to approvals too, because nobody is there to answer them.
func TestAnApprovalStillRespectsTheNight(t *testing.T) {
	g := gate("", "", 3, 12)
	if g.spend(asking(), g.at()) {
		t.Fatalf("rang for an approval at 03:12")
	}
}

func TestBothSettingsCanBeTurnedOff(t *testing.T) {
	always := gate("off", "off", 3, 12)
	if !always.spend(finished(), always.at()) {
		t.Fatalf("a turned-off quiet window still held the banner")
	}
	for i := 0; i < 40; i++ {
		if !always.spend(finished(), always.at()) {
			t.Fatalf("a turned-off budget stopped at %d", i)
		}
	}
}

// The settings file is hand-editable, so a typo has to land on the default and
// not on a week of silence.
func TestABadQuietWindowFallsBackToTheDefault(t *testing.T) {
	for _, spec := range []string{"", "nonsense", "25-7", "22-99", "-"} {
		from, to := prefs{Quiet: spec}.quietWindow()
		if from != 22 || to != 8 {
			t.Fatalf("%q read as %d-%d, want the 22-8 default", spec, from, to)
		}
	}
	if from, to := (prefs{Quiet: "23-6"}).quietWindow(); from != 23 || to != 6 {
		t.Fatalf("23-6 read as %d-%d", from, to)
	}
	if from, to := (prefs{Quiet: "off"}).quietWindow(); from != 0 || to != 0 {
		t.Fatalf("off read as %d-%d", from, to)
	}
}

func TestTheBudgetDefaultsToFiveAndAcceptsANumber(t *testing.T) {
	for spec, want := range map[string]int{"": 5, "off": 0, "8": 8, "0": 5, "abc": 5, " 7 ": 7} {
		if got := (prefs{DailyCap: spec}).bannerBudget(); got != want {
			t.Fatalf("cap %q = %d, want %d", spec, got, want)
		}
	}
}

// The same rule end to end: a snapshot that finishes a turn at 23:40 does not
// reach the banner, and the rail is where the news stays.
func TestASnapshotAtNightDoesNotRing(t *testing.T) {
	i18n.Set(i18n.ZH)
	a := fixtureApp(t, false)
	a.delivery = gate("", "", 23, 40)
	var got []alert
	a.banner = func(al alert) { got = append(got, al) }
	state := &protocol.State{Surfaces: []protocol.Surface{
		{SurfaceID: "oc_12", ConversationID: 12, Title: "Deploy checklist"},
	}}

	a.applySnapshot(&snapshotResult{ok: true, state: state, tasksOK: true,
		tasks: []protocol.Task{{ConversationID: 12, State: "running"}}})
	a.applySnapshot(&snapshotResult{ok: true, state: state, tasksOK: true})

	if len(got) != 0 {
		t.Fatalf("the window rang at 23:40: %#v", got)
	}
}
