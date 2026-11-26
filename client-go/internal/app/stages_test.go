package app

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"pilot/internal/api"
	"pilot/internal/i18n"
	"pilot/internal/protocol"
)

// stageBridge answers the pipeline strip, so the window can be checked for what it
// says about a step that is blocked.
func stageFixture(t *testing.T, body string) *App {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/stages" {
			_, _ = w.Write([]byte(body))
			return
		}
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(server.Close)
	a := fixtureApp(t, false)
	a.api = api.New(server.URL, "")
	a.active = 11
	a.loadStages(11)
	a.drain()
	return a
}

// TestStageStripSaysWhatBlocks: the reason a merge cannot happen is on the strip,
// not in a tooltip, and the cap is repeated as a count.
func TestStageStripSaysWhatBlocks(t *testing.T) {
	i18n.Set(i18n.ZH)
	a := stageFixture(t, `{"branch":"feat/x","base":"release/test","target":"release/online",`+
		`"rounds":3,"roundLimit":3,"stages":[`+
		`{"key":"dev","label":"开发","state":"done","detail":"2 个提交"},`+
		`{"key":"test","label":"提测到 release/test","state":"done"},`+
		`{"key":"release","label":"发上线申请","state":"blocked","detail":"!3165","blockedBy":"机器评审已评 3 轮（上限 3）","nextAction":"人工 review 或改需求"},`+
		`{"key":"ship","label":"上线","state":"todo"}]}`)
	tt := ui.NewTester(a.View, 1180, 820)
	tt.Frame()
	// Folded, the strip is the blocked step and what to do about it: that is the
	// whole reason it is on screen while it is stuck.
	for _, want := range []string{"发上线申请", "人工 review 或改需求", "评审第 3/3 轮"} {
		if !tt.HasText(want) {
			t.Fatalf("the strip does not say %q; drew %v", want, tt.Texts())
		}
	}
	if err := tt.Click("展开流程"); err != nil {
		t.Fatalf("the whole pipeline is not one click away: %v", err)
	}
	tt.Frame()
	for _, want := range []string{"开发", "提测到 release/test", "上线"} {
		if !tt.HasText(want) {
			t.Fatalf("the unfolded strip does not say %q; drew %v", want, tt.Texts())
		}
	}
	writePNG(t, "/tmp/clientv2-stages.png", tt.Image())
}

// TestStageStripFoldsToTheStepThatMoves: four steps always drawn is a map, and the
// reader wants the one step they are on. The rest is behind the caret.
func TestStageStripFoldsToTheStepThatMoves(t *testing.T) {
	i18n.Set(i18n.ZH)
	a := stageFixture(t, `{"branch":"feat/x","base":"release/test","target":"release/online",`+
		`"rounds":0,"roundLimit":3,"stages":[`+
		`{"key":"dev","label":"开发","state":"done","detail":"2 个提交"},`+
		`{"key":"test","label":"提测到 release/test","state":"active","nextAction":"合到 release/test 并推送"},`+
		`{"key":"release","label":"发上线申请","state":"todo"},`+
		`{"key":"ship","label":"上线","state":"todo"}]}`)
	tt := ui.NewTester(a.View, 1180, 820)
	tt.Frame()
	if !tt.HasText("提测到 release/test") || !tt.HasText("2/4") {
		t.Fatalf("the folded strip does not show the live step; drew %v", tt.Texts())
	}
	for _, gone := range []string{"发上线申请", "开发", "上线"} {
		if tt.HasText(gone) {
			t.Errorf("the folded strip still draws %q; drew %v", gone, tt.Texts())
		}
	}
	writePNG(t, "/tmp/clientv2-stages-folded.png", tt.Image())
}

// TestStageStripIsSilentWithoutAChat: no report means no strip, rather than a row
// of grey steps that claims the work has not started.
func TestStageStripIsSilentWithoutAChat(t *testing.T) {
	i18n.Set(i18n.ZH)
	a := fixtureApp(t, false)
	a.stageStrip = nil
	tt := ui.NewTester(a.View, 1180, 820)
	tt.Frame()
	if a.stageStrip != nil {
		t.Fatal("a strip appeared without a report")
	}
}

// TestStageAskWritesTheNextStep: the strip knows what comes next, so the button
// writes it into the composer — and stops there, because a merge is worth
// reading twice before it is sent.
func TestStageAskWritesTheNextStep(t *testing.T) {
	i18n.Set(i18n.ZH)
	a := stageFixture(t, `{"branch":"feat/x","base":"release/test","target":"release/online",`+
		`"rounds":0,"roundLimit":3,"stages":[`+
		`{"key":"dev","label":"开发","state":"done","detail":"2 个提交"},`+
		`{"key":"test","label":"提测到 release/test","state":"active","nextAction":"合到 release/test 并推送"},`+
		`{"key":"release","label":"发上线申请","state":"todo"},`+
		`{"key":"ship","label":"上线","state":"todo"}]}`)
	tt := ui.NewTester(a.View, 1180, 820)
	tt.Frame()
	writePNG(t, "/tmp/clientv2-stages-active.png", tt.Image())
	if err := tt.Click("让它做"); err != nil {
		t.Fatalf("the ask button is not clickable: %v", err)
	}
	tt.Frame()
	if a.draft == "" {
		t.Fatal("nothing was written into the composer")
	}
	for _, want := range []string{"feat/x", "release/test"} {
		if !strings.Contains(a.draft, want) {
			t.Errorf("the sentence does not name %q: %q", want, a.draft)
		}
	}
	if len(a.draft) == 0 {
		t.Error("the composer should hold the proposal, not be empty")
	}
}

// TestStagesFollowTheBranch: the strip is derived, so the two moments the
// repository can move under it — a turn ending, a write landing — have to ask
// again, or it keeps saying "not pushed yet" after a push.
func TestStagesFollowTheBranch(t *testing.T) {
	i18n.Set(i18n.ZH)
	asks := make(chan string, 8)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/stages" {
			select {
			case asks <- r.URL.RawQuery:
			default:
			}
			_, _ = w.Write([]byte(`{"branch":"feat/x","base":"release/test","target":"feature/server",` +
				`"rounds":0,"roundLimit":3,"stages":[{"key":"dev","label":"开发","state":"active"}]}`))
			return
		}
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(server.Close)
	a := fixtureApp(t, false)
	a.api = api.New(server.URL, "")
	a.active = 11
	th := a.thread(11)
	th.live.Run = []byte(`{"runId":"r1"}`)
	// A turn ends: the same chat comes back with no run in it.
	a.post(event{kind: evFrame, frame: &frameResult{
		convID: 11,
		frame: protocol.Frame{Value: &protocol.FrameValue{
			Docs: map[string]protocol.LiveDoc{"pi.live": {Run: nil}},
		}},
	}})
	deadline := time.Now().Add(2 * time.Second)
	saw := false
	for time.Now().Before(deadline) && !saw {
		a.drain()
		select {
		case <-asks:
			saw = true
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}
	if !saw {
		t.Fatal("the strip did not ask again after the turn ended")
	}
}

// TestStageStripKeepsItsWordsWhole: with the drawer open the strip has about 460
// points, and a single row there squeezed 提测到 release/test into a column of
// one hanzi per line. MaxLines and a second row are what keep it a sentence.
func TestStageStripKeepsItsWordsWhole(t *testing.T) {
	i18n.Set(i18n.ZH)
	a := stageFixture(t, `{"branch":"feat/x","base":"release/test","target":"release/online",`+
		`"rounds":1,"roundLimit":3,"stages":[`+
		`{"key":"dev","label":"开发","state":"done","detail":"2 个提交"},`+
		`{"key":"test","label":"提测到 release/test","state":"active","nextAction":"合到 release/test 并推送"},`+
		`{"key":"release","label":"发上线申请","state":"todo"},`+
		`{"key":"ship","label":"上线","state":"todo"}]}`)
	a.changes.open = true
	a.stageOpen = true
	tt := ui.NewTester(a.View, 1180, 820)
	tt.Frame()
	for _, want := range []string{"提测到 release/test", "发上线申请", "合到 release/test 并推送"} {
		if !tt.HasText(want) {
			t.Errorf("the strip broke %q into pieces; drew %v", want, tt.Texts())
		}
	}
	writePNG(t, "/tmp/clientv2-stages-narrow.png", tt.Image())
}
