package app

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"pitta/internal/i18n"
	"pitta/internal/protocol"
)

// railFixture is an app whose rail has a row of every kind the list draws: a
// chat its owner named, a client chat nobody named, a Feishu surface nobody
// named, a fork, and a chat of another project.
func railFixture(t *testing.T) *App {
	t.Helper()
	// The app reads its preferences from the home directory: give it one of
	// its own, so a developer's pins cannot fail the test.
	t.Setenv("HOME", t.TempDir())
	a := New(Config{Home: "/srv/pitta"})
	// The language and the folded header outlive a frame: put both back so the
	// package's other tests see the process they expect.
	lang := i18n.Current()
	t.Cleanup(func() {
		i18n.Set(lang)
		a.othersOpen = true
	})
	i18n.Set(i18n.EN)
	a.othersOpen = true
	a.project = "alpha"
	a.projects = []protocol.Project{
		{ID: "alpha", Name: "Alpha"},
		{ID: "beta", Name: "Beta"},
	}
	a.surfaces = []protocol.Surface{
		{ConversationID: 12, SurfaceID: "client:12", Kind: "client", ProjectID: "alpha", Cost: 0.5},
		{ConversationID: 9, SurfaceID: "oc_feishu9", Kind: "feishu", ProjectID: "alpha", Title: "Feishu planning", Cost: 2},
		{ConversationID: 7, SurfaceID: "client:7", ProjectID: "alpha"},
		{ConversationID: 4, SurfaceID: "client:4", Kind: "client", ProjectID: "alpha", ThreadParent: 12, Title: "Fork of 12"},
		{ConversationID: 3, SurfaceID: "oc_abcdef12", Kind: "feishu", ProjectID: "alpha"},
		{ConversationID: 2, SurfaceID: "client:2", Kind: "client", ProjectID: "beta", Title: "Beta chat"},
	}
	a.current = 12
	return a
}

// railTest draws the rail once and returns the tester showing it.
func railTest(t *testing.T, a *App) *ui.Tester {
	t.Helper()
	tt := ui.NewTester(func(c *ui.Context) { a.rail(c) }, 260, 720)
	tt.Frame()
	return tt
}

// textIndex is where the first text containing s was built, or -1: rows are
// listed in the order the rail builds them.
func textIndex(texts []string, s string) int {
	for i, text := range texts {
		if strings.Contains(text, s) {
			return i
		}
	}
	return -1
}

// TestRailTitlesFallBackToTheKind is the rail's title rule: the name its owner
// gave the chat, else the name the client gives the chats it opened, else the
// surface the chat talks on.
func TestRailTitlesFallBackToTheKind(t *testing.T) {
	a := railFixture(t)
	tt := railTest(t, a)
	for _, want := range []string{"New chat #12", "New chat #7", "Feishu planning", "Feishu abcdef"} {
		if !tt.HasText(want) {
			t.Fatalf("rail is missing %q, shows %q", want, tt.Texts())
		}
	}
	// Rows spend: cents under a dollar, tenths above it, as the rail.
	for _, want := range []string{"$0.50", "$2.0"} {
		if !tt.HasText(want) {
			t.Fatalf("rail is missing the spend %q, shows %q", want, tt.Texts())
		}
	}
}

// TestRailNestsForksUnderTheirChat draws a fork right under the chat it forked
// from, however the list sorts the chats.
func TestRailNestsForksUnderTheirChat(t *testing.T) {
	a := railFixture(t)
	tt := railTest(t, a)
	texts := tt.Texts()
	parent, fork := textIndex(texts, "New chat #12"), textIndex(texts, "Fork of 12")
	if parent < 0 || fork < 0 || fork < parent {
		t.Fatalf("fork at %d, its chat at %d, want the fork under it: %q", fork, parent, texts)
	}
}

// TestRailSearchNarrowsTheList keeps only the chats the search matches.
func TestRailSearchNarrowsTheList(t *testing.T) {
	a := railFixture(t)
	a.query = "planning"
	tt := railTest(t, a)
	if !tt.HasText("Feishu planning") {
		t.Fatalf("rail lost the matching chat, shows %q", tt.Texts())
	}
	if tt.HasText("New chat #12") {
		t.Fatalf("rail kept a chat the search does not match, shows %q", tt.Texts())
	}
	a.query = "nothing matches this"
	tt.Frame()
	if !tt.HasText(i18n.T("noHits")) {
		t.Fatalf("rail is missing the empty search's answer, shows %q", tt.Texts())
	}
}

// TestRailFilterShowsOneKind keeps only the chats of the chosen kind.
func TestRailFilterShowsOneKind(t *testing.T) {
	a := railFixture(t)
	a.prefs.SurfaceFilter = "feishu"
	tt := railTest(t, a)
	if !tt.HasText("Feishu planning") {
		t.Fatalf("rail lost the Feishu chat, shows %q", tt.Texts())
	}
	if tt.HasText("New chat #12") {
		t.Fatalf("rail kept a client chat under the Feishu filter, shows %q", tt.Texts())
	}
}

// TestRailPinsChatAboveTheRest puts a pinned chat above the chats that are
// merely newer.
func TestRailPinsChatAboveTheRest(t *testing.T) {
	a := railFixture(t)
	a.prefs.Pinned = []int{3}
	tt := railTest(t, a)
	texts := tt.Texts()
	pinned, newest := textIndex(texts, "Feishu abcdef"), textIndex(texts, "New chat #12")
	if pinned < 0 || newest < 0 || pinned > newest {
		t.Fatalf("pinned chat at %d, the newest chat at %d, want the pinned one first: %q", pinned, newest, texts)
	}
}

// TestRailShowsTheDownSocket says why the list cannot refresh while the agent
// is not answering.
func TestRailShowsTheDownSocket(t *testing.T) {
	a := railFixture(t)
	a.conn = ConnDown
	a.downReason = "dial tcp 127.0.0.1:8787: connect: connection refused"
	a.retry = 3
	tt := railTest(t, a)
	for _, want := range []string{i18n.T("disconnected"), "connection refused", "retried 3"} {
		if !tt.HasText(want) {
			t.Fatalf("rail is missing %q, shows %q", want, tt.Texts())
		}
	}
}

// TestRailListsTheOwnersTasks shows the tasks the owner started, and not the
// bridge's own.
func TestRailListsTheOwnersTasks(t *testing.T) {
	a := railFixture(t)
	a.tasks = []protocol.Task{
		{ID: 1, Kind: "app.feature", State: "running", Title: "Ship the rail"},
		{ID: 2, Kind: "app.usage-sampler", State: "sleeping"},
	}
	tt := railTest(t, a)
	if !tt.HasText("Ship the rail") || !tt.HasText("running") {
		t.Fatalf("rail is missing the running task, shows %q", tt.Texts())
	}
	if tt.HasText("app.usage-sampler") {
		t.Fatalf("rail lists the bridge's own task, shows %q", tt.Texts())
	}
}

// TestRailFoldsOtherProjects keeps the chats of the other projects behind one
// header, and shows them when it is open.
func TestRailFoldsOtherProjects(t *testing.T) {
	a := railFixture(t)
	tt := railTest(t, a)
	if !tt.HasText(i18n.T("otherProjects")) || !tt.HasText("Beta chat") {
		t.Fatalf("rail is missing the other project's chat or its header, shows %q", tt.Texts())
	}
	a.othersOpen = false
	tt.Frame()
	if !tt.HasText(i18n.T("otherProjects")) {
		t.Fatalf("rail lost the folded header, shows %q", tt.Texts())
	}
	if tt.HasText("Beta chat") {
		t.Fatalf("rail kept the other project's chat while folded, shows %q", tt.Texts())
	}
}
