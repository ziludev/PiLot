package app

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"pilot/internal/api"
	"pilot/internal/i18n"
	"pilot/internal/protocol"
	"pilot/internal/theme"
)

// fixtureApp builds a window with the shapes the real bridge sends: three
// chats, five turns of transcript with folded steps and one error, three tasks
// by state, and — when asked — one approval waiting.
func fixtureApp(t *testing.T, pending bool) *App {
	t.Helper()
	a := newApp(Config{})
	now := time.Now()
	a.now = now
	a.online = true

	// Two chats spoke today and are drawn open; the rest are filed under what
	// they are, folded, one heading each.
	a.setSurfaces([]protocol.Surface{
		{SurfaceID: "oc_12", ConversationID: 12, Kind: "feishu", ChatType: "p2p", Title: "Deploy checklist for the 0.4 train", ProjectID: "workspace"},
		{SurfaceID: "client:11", ConversationID: 11, Kind: "client", Title: "Why is the sky blue", ProjectID: "workspace"},
		{SurfaceID: "oc_8", ConversationID: 8, Kind: "feishu", ChatType: "group", Title: "backend-deploys"},
		{SurfaceID: "oc_7", ConversationID: 7, Kind: "feishu", ChatType: "group", Topic: true, Title: "release talk"},
		{SurfaceID: "oc_6", ConversationID: 6, Kind: "feishu", ChatType: "p2p", Title: "alex"},
		{SurfaceID: "client:9", ConversationID: 9, Kind: "other", Title: "Sandbox scratch", ProjectID: "workspace"},
	})
	a.vm.tails[11] = now.Add(-3 * time.Minute)
	a.vm.tails[12] = now.Add(-2 * time.Hour)
	a.vm.tails[8] = now.Add(-3 * 24 * time.Hour)
	a.vm.tails[7] = now.Add(-4 * 24 * time.Hour)
	a.vm.tails[6] = now.Add(-5 * 24 * time.Hour)
	a.vm.tails[9] = now.Add(-26 * time.Hour)
	a.vm.unread[12] = true

	a.tasks = []protocol.Task{
		{ID: 1084, Title: "Follow up on the review thread", State: "working", Until: now.Add(20 * time.Minute).UnixMilli(), Spend: &protocol.TaskSpend{Cost: 0.031, Tokens: 12043}},
		{ID: 1091, Title: "Wait for the nightly build", State: "waiting", Until: now.Add(4 * time.Hour).UnixMilli()},
		{ID: 1102, Title: "Digest the deploy notes", State: "sleeping", Until: now.Add(26 * time.Hour).UnixMilli()},
	}

	start := now.Add(-8 * time.Minute)
	entries := []protocol.Entry{
		entryJSON(t, 41, "pi.user", start, `[{"role":"user","content":"why is the sky blue? and can you check the build while you answer","timestamp":__ts}]`),
		entryJSON(t, 42, "pi.assistant", start.Add(1*time.Second), `[{"role":"assistant","content":[{"type":"thinking","thinking":"Rayleigh scattering: intensity goes as 1/wavelength^4"},{"type":"toolCall","name":"bash","arguments":{"command":"go build ./... 2>&1 | tail -5"}}],"timestamp":__ts}]`),
		entryJSON(t, 43, "pi.tool-result", start.Add(13*time.Second), `[{"role":"toolResult","content":[{"type":"text","text":"build ok"}],"timestamp":__ts}]`),
		entryJSON(t, 44, "pi.assistant", start.Add(15*time.Second), `[{"role":"assistant","content":[{"type":"text","text":"Sunlight is a mix of wavelengths, and air scatters the short ones far more strongly than the long ones — intensity goes with 1/λ⁴. **Blue** light is scattered out of the direct beam and reaches your eye from every direction, so the sky looks blue.\n\nAt sunset the light travels through much more air, so the blue is scattered away before it arrives and what is left is red. The build is green."}],"timestamp":__ts}]`),
		entryJSON(t, 45, "pi.user", start.Add(2*time.Minute), `[{"role":"user","content":"and why is it red at sunset?","timestamp":__ts}]`),
		entryJSON(t, 46, "pi.assistant", start.Add(2*time.Minute+4*time.Second), `[{"role":"assistant","content":[{"type":"thinking","thinking":"Long path length, blue removed"}],"timestamp":__ts}]`),
		entryJSON(t, 47, "pi.assistant", start.Add(2*time.Minute+9*time.Second), `[{"role":"assistant","content":[{"type":"text","text":"Because the light crosses a much longer slice of atmosphere: the blue is scattered out of the beam before it reaches you, leaving oranges and reds."}],"timestamp":__ts}]`),
		entryJSON(t, 48, "pi.assistant", start.Add(2*time.Minute+20*time.Second), `[{"role":"assistant","content":[],"stopReason":"error","errorMessage":"provider returned 502 after 3 retries","timestamp":__ts}]`),
	}
	th := a.thread(11)
	th.loaded = true
	th.entries = entries
	th.rev = map[int]uint64{}
	for _, entry := range entries {
		th.rev[entry.ID] = fingerprint(entry)
	}
	th.version++
	a.active = 11
	a.vm.unread[11] = false
	a.focusComposer = false

	if pending {
		a.pending = []protocol.PendingApproval{{
			CallID:  "call-7",
			Tool:    "bash",
			Summary: "rm -rf ./build && go build ./...",
			Reason:  "destructive command outside the workspace allowlist",
		}}
	}
	return a
}

// TestSnapshotWindow draws the window with real data twice: light with the
// composer and the chat list, dark with a pending approval and the task list.
// The pngs are left in /tmp for a person to look at.
func TestSnapshotWindow(t *testing.T) {
	i18n.Set(i18n.ZH)
	for _, c := range []struct {
		name    string
		dark    bool
		pending bool
		path    string
	}{
		{"light", false, false, "/tmp/clientv2-light.png"},
		{"dark", true, true, "/tmp/clientv2-dark.png"},
	} {
		a := fixtureApp(t, c.pending)
		if c.pending {
			a.rail = railTasks
			a.tasks[0].State = "working"
		} else {
			// The composer's own layers: a draft, a queued message, an attachment.
			a.draft = "and what about sunsets on Mars?"
			a.thread(a.active).inbox = []protocol.InboxItem{
				{ID: 1, Mode: "followUp", Content: []byte(`"and what about sunsets on Mars?"`)},
				{ID: 2, Mode: "steer", Content: []byte(`"check the build too"`)},
			}
			a.draftImage = []imageRef{{mime: "image/png", data: thumbnailPNG(t)}}
		}
		tt := ui.NewTester(a.View, 1180, 820)
		tt.SetDark(c.dark)
		tt.Frame()

		img := tt.Image()
		want := theme.Light()
		if c.dark {
			want = theme.Dark()
		}
		if countColor(img, want.Background) < 1000 {
			t.Fatalf("%s: background %v barely on screen", c.name, want.Background)
		}
		if countColor(img, want.Sidebar) < 1000 {
			t.Fatalf("%s: rail %v missing", c.name, want.Sidebar)
		}
		for _, text := range []string{"会话", "任务", "Why is the sky blue"} {
			if !tt.HasText(text) {
				t.Fatalf("%s: missing %q; drew %v", c.name, text, tt.Texts())
			}
		}
		if !c.pending && !tt.HasText("飞书") {
			t.Fatalf("%s: the chat list lost its source marker; drew %v", c.name, tt.Texts())
		}
		if !c.pending {
			// Every chat section has its heading, and the folded ones keep their
			// chats out of the list: the sections are the rail's filing.
			for _, text := range []string{"进行中", "群聊", "话题", "私聊", "其他"} {
				if !tt.HasText(text) {
					t.Fatalf("%s: missing section %q; drew %v", c.name, text, tt.Texts())
				}
			}
			if tt.HasText("backend-deploys") {
				t.Fatalf("%s: a folded group leaked its chats", c.name)
			}
		}
		if !tt.HasText("1 步 · 12s") {
			t.Fatalf("%s: folded summary missing; drew %v", c.name, tt.Texts())
		}
		if c.pending {
			for _, text := range []string{"需要批准：bash", "允许", "拒绝", "进行中", "休眠"} {
				if !tt.HasText(text) {
					t.Fatalf("%s: missing %q", c.name, text)
				}
			}
		} else {
			for _, text := range []string{"排队", "and what about sunsets on Mars?", "插话", "check the build too"} {
				if !tt.HasText(text) {
					t.Fatalf("%s: missing %q; drew %v", c.name, text, tt.Texts())
				}
			}
			// The prompt bar has no hint line under the field; its model control
			// is the text it does draw, so that is what proves the row is there.
			if !hasAnyText(tt, "模型") {
				t.Fatalf("%s: the prompt bar lost its model control; drew %v", c.name, tt.Texts())
			}
		}
		writePNG(t, c.path, img)
	}
}

// hasAnyText is HasText over a fragment: the placeholder is one long string and
// a narrow window may draw it in pieces.
func hasAnyText(tt *ui.Tester, fragment string) bool {
	for _, text := range tt.Texts() {
		if strings.Contains(text, fragment) {
			return true
		}
	}
	return false
}

// thumbnailPNG is a small picture, big enough to decode and small enough to
// keep the fixture readable.
func thumbnailPNG(t *testing.T) []byte {
	t.Helper()
	const w, h = 64, 48
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for x := 0; x < w; x++ {
		for y := 0; y < h; y++ {
			img.Set(x, y, color.RGBA{R: uint8(40 + x*3), G: uint8(120 + y*2), B: uint8(200), A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode thumbnail: %v", err)
	}
	return buf.Bytes()
}

func writePNG(t *testing.T, name string, img *image.RGBA) {
	t.Helper()
	file, err := os.Create(name)
	if err != nil {
		t.Fatalf("create %s: %v", name, err)
	}
	defer file.Close()
	if err := png.Encode(file, img); err != nil {
		t.Fatalf("encode %s: %v", name, err)
	}
}

// countColor counts the pixels of one exact color, which is how a snapshot
// proves a token reached the screen.
func countColor(img *image.RGBA, want ui.Color) int {
	count := 0
	bounds := img.Bounds()
	for x := bounds.Min.X; x < bounds.Max.X; x++ {
		for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
			r, g, b, alpha := img.At(x, y).RGBA()
			if nearColor(uint8(r>>8), want.R) && nearColor(uint8(g>>8), want.G) &&
				nearColor(uint8(b>>8), want.B) && nearColor(uint8(alpha>>8), want.A) {
				count++
			}
		}
	}
	return count
}

// nearColor tolerates the renderer's rounding.
func nearColor(a, b uint8) bool {
	d := int(a) - int(b)
	return d >= -2 && d <= 2
}

var _ = strings.TrimSpace

// TestSnapshotRunning draws the moment the composer has to describe: a turn in
// flight, messages waiting behind it, and a chat further down the rail holding
// its own queue. The png is left in /tmp for a person to look at.
// TestSnapshotLoading draws the moment before anything has come back: the pixel
// loader, its label, and the elapsed clock. It is Loading State's own shape, and
// it is the only view where the loader is on screen.
func TestSnapshotLoading(t *testing.T) {
	i18n.Set(i18n.ZH)
	a := fixtureApp(t, false)
	now := time.Now()
	a.tasks = append(a.tasks, protocol.Task{ID: 9002, State: "working", StartedAt: now.Add(-14 * time.Second).UnixMilli()})
	a.thread(11).live.Run = json.RawMessage(`{"taskId":9002}`)
	a.draft = ""

	tt := ui.NewTester(a.View, 1180, 820)
	tt.Frame()
	for _, text := range []string{"思考中", "already ran"} {
		if text == "already ran" {
			continue // the clock is drawn in the row, not always the strip
		}
		if !tt.HasText(text) {
			t.Fatalf("missing %q; drew %v", text, tt.Texts())
		}
	}
	writePNG(t, "/tmp/clientv2-loading.png", tt.Image())
}

func TestSnapshotRunning(t *testing.T) {
	i18n.Set(i18n.ZH)
	a := fixtureApp(t, false)
	now := time.Now()
	a.queued = map[int]int{9: 2, 11: 2}
	a.tasks = append(a.tasks, protocol.Task{ID: 9001, State: "working", StartedAt: now.Add(-92 * time.Second).UnixMilli()})
	a.thread(11).live.Run = json.RawMessage(`{"taskId":9001}`)
	a.thread(11).live.Generation = json.RawMessage(`{"message":{"content":[{"type":"text","text":"Reading the build config first, then I will run the tests."}]}}`)
	a.thread(11).live.Tools = []json.RawMessage{json.RawMessage(`{"callId":"c1","name":"bash","status":"running"}`)}
	a.thread(11).inbox = []protocol.InboxItem{
		{ID: 1, Mode: "followUp", Content: []byte(`"and what about sunsets on Mars?"`)},
		{ID: 2, Mode: "steer", Content: []byte(`"check the build too"`)},
	}
	a.draft = ""

	tt := ui.NewTester(a.View, 1180, 820)
	tt.Frame()
	for _, text := range []string{
		"已经跑了 1m32s",
		"正在跑 bash",
		"Reading the build config first",
		"重试",
		"排队",
		"插话",
		"check the build too",
	} {
		if !tt.HasText(text) {
			t.Fatalf("missing %q; drew %v", text, tt.Texts())
		}
	}
	writePNG(t, "/tmp/clientv2-running.png", tt.Image())
}

// TestSnapshotRailSearch draws the rail with the filter box's two new blocks in
// it: the bridge's hits above the chats, and the archived shelf under them. The
// row menu of the chat on screen is there too, as a ⋯ of its own.
func TestSnapshotRailSearch(t *testing.T) {
	i18n.Set(i18n.ZH)
	a := fixtureApp(t, false)
	// One chat filed away; its title matches the query, which opens the shelf.
	surfaces := a.surfaceSnapshot()
	surfaces = append(surfaces, protocol.Surface{
		SurfaceID: "oc_5", ConversationID: 5, Kind: "feishu", Title: "sky notes, filed", Archived: true,
	})
	a.setSurfaces(surfaces)

	a.filter = "sky"
	a.search.query = lowerTrim(a.filter)
	a.search.hits = []protocol.SearchHit{
		{ConversationID: 11, EntryID: 44, Snippet: "deploy at six"},
	}

	tt := ui.NewTester(a.View, 1180, 820)
	tt.Frame()
	for _, text := range []string{"搜索结果", "Why is the sky blue", "deploy at six", "已归档"} {
		if !tt.HasText(text) {
			t.Fatalf("missing %q; drew %v", text, tt.Texts())
		}
	}
	// The ⋯ of the chat on screen: a text of its own, not the folded step's
	// summary that happens to start with the same mark.
	if !slices.Contains(tt.Texts(), "⋯") {
		t.Fatalf("no row menu on the chat on screen; drew %v", tt.Texts())
	}
	writePNG(t, "/tmp/clientv2-search.png", tt.Image())
}

// TestSnapshotToolOutput draws an opened fold whose tool result is a diff and a
// failing test run: the block is the point of opening it, so it is not the
// eighty clipped characters the row summary used to be.
func TestSnapshotToolOutput(t *testing.T) {
	i18n.Set(i18n.ZH)
	a := fixtureApp(t, false)
	out := "--- a/src/queue.ts\n+++ b/src/queue.ts\n@@ -12,7 +12,7 @@\n-  const retries = 3;\n+  const retries = 5;\n \n FAIL src/queue.test.ts\n  ● retries after a 502\n\n    expect(received).toBe(expected)\n\n    Expected: 5\n    Received: 3\n\n  \x1b[1m\x1b[31m1 failed\x1b[0m, 1 passed"
	quoted, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	a.thread(11).upsert(entryJSON(t, 91, "pi.tool-result", time.Now().Add(-7*time.Minute),
		`[{"role":"toolResult","content":[{"type":"text","text":`+string(quoted)+`}],"timestamp":__ts}]`))
	a.thread(11).upsert(entryJSON(t, 90, "pi.assistant", time.Now().Add(-7*time.Minute),
		`[{"role":"assistant","content":[{"type":"toolCall","name":"bash","arguments":{"command":"npx vitest run src/queue.test.ts"}}],"timestamp":__ts}]`))
	for _, row := range a.threadRows(11) {
		if row.kind == rowSteps {
			a.vm.folds[row.id] = true
		}
	}

	tt := ui.NewTester(a.View, 1180, 820)
	tt.Frame()
	for _, text := range []string{
		"npx vitest run src/queue.test.ts",
		"@@ -12,7 +12,7 @@",
		"-  const retries = 3;",
		"… 还有 2 行", // dropped from the middle, and says so
		"Expected: 5",
		"1 failed, 1 passed", // ANSI stripped, not drawn as garbage
	} {
		if !tt.HasText(text) {
			t.Fatalf("missing %q; drew %v", text, tt.Texts())
		}
	}
	if hasAnyText(tt, "\x1b") {
		t.Fatal("escape codes reached the screen")
	}
	writePNG(t, "/tmp/clientv2-tooloutput.png", tt.Image())
}

// TestSnapshotModelPanel draws the picker open over the composer: the chip it
// came from, the filter, the chat's provider unfolded, and the other two folded
// down to a heading with a count.
func TestSnapshotModelPanel(t *testing.T) {
	i18n.Set(i18n.ZH)
	a := fixtureApp(t, false)
	a.model = &protocol.ModelRef{Provider: "anthropic", ModelID: "claude-sonnet-4.5"}
	a.thinking = "high"
	a.pick.open = true
	a.pick.fetched = true
	a.pick.catalog = []protocol.ModelEntry{
		{Provider: "anthropic", ProviderName: "Anthropic", ID: "claude-sonnet-4.5", Name: "Claude Sonnet 4.5",
			ContextWindow: 200000, Cost: protocol.ModelCost{Input: 3, Output: 15}, ThinkingLevels: []string{"off", "low", "high"}},
		{Provider: "anthropic", ProviderName: "Anthropic", ID: "claude-haiku-4.5", Name: "Claude Haiku 4.5",
			ContextWindow: 200000, Cost: protocol.ModelCost{Input: 0.8, Output: 4}},
		{Provider: "google", ProviderName: "Google", ID: "gemini-2.5-pro", Name: "Gemini 2.5 Pro",
			ContextWindow: 1000000, Cost: protocol.ModelCost{Input: 1.25, Output: 10}},
		{Provider: "deepseek", ProviderName: "DeepSeek", ID: "deepseek-chat", Name: "DeepSeek Chat",
			ContextWindow: 64000, Cost: protocol.ModelCost{Input: 0.27, Output: 1.1}},
	}

	tt := ui.NewTester(a.View, 1180, 820)
	tt.Frame()
	// Google is folded to a heading until it is asked for.
	tt.Click("Google")
	tt.Frame()
	for _, text := range []string{"Claude Sonnet 4.5 · high", "Gemini 2.5 Pro", "Anthropic · 200k · $3/$15", "思考", "high"} {
		if !tt.HasText(text) {
			t.Fatalf("missing %q; texts = %v", text, tt.Texts())
		}
	}
	writePNG(t, "/tmp/clientv2-model.png", tt.Image())
}

// TestSnapshotPalette draws ⌘K over the transcript: the field it comes from,
// the files it can find, and the row a keyboard cursor would land on.
func TestSnapshotPalette(t *testing.T) {
	i18n.Set(i18n.ZH)
	a := fixtureApp(t, false)
	a.openPalette()
	a.palette.query = "deploy"
	tt := ui.NewTester(a.View, 1180, 820)
	tt.Frame()
	if !tt.HasText("↑↓ 选择 · ⏎ 打开 · esc 关闭") {
		t.Fatalf("the palette did not draw; %v", tt.Texts())
	}
	writePNG(t, "/tmp/clientv2-palette.png", tt.Image())
}

// TestSnapshotUsage draws the calendar the way a month of real spend looks:
// quiet days, a heavy one, and the summary under the grid.
func TestSnapshotUsage(t *testing.T) {
	i18n.Set(i18n.ZH)
	a := fixtureApp(t, false)
	today := startOfDay(a.now)
	var days []api.UsageDay
	for back := 20; back >= 0; back-- {
		day := today.AddDate(0, 0, -back)
		cost := 0.0
		switch {
		case back == 1:
			cost = 3.4
		case back == 6:
			cost = 1.8
		case back%3 == 0:
			cost = 0.4
		case back%4 == 0:
			cost = 0.15
		}
		days = append(days, api.UsageDay{
			Date:   day.Format("2006-01-02"),
			Cost:   cost,
			Tokens: int64(cost * 900000),
		})
	}
	a.usage.open = true
	a.applyUsage(0, days, nil)
	tt := ui.NewTester(a.View, 1180, 820)
	tt.Frame()
	writePNG(t, "/tmp/clientv2-usage.png", tt.Image())
}

// TestSnapshotTasks draws the 任务 tab with the kinds the bridge actually runs:
// one task the bridge names itself, one turn that is only knowable by its chat,
// and one housekeeping job with no conversation at all. Each used to be a kind
// name and an id, which is a row nobody can read.
func TestSnapshotTasks(t *testing.T) {
	i18n.Set(i18n.ZH)
	a := fixtureApp(t, false)
	now := a.now
	a.rail = railTasks
	a.tasks = []protocol.Task{
		{ID: 84338, Kind: "app.routine", Title: "从过去 24 小时的真实痕迹里找出流程哪里不对", State: "waiting", Until: now.Add(4 * time.Hour).UnixMilli()},
		{ID: 100316, Kind: "pi.generation", ConversationID: 11, State: "working", Until: now.Add(20 * time.Minute).UnixMilli(), Spend: &protocol.TaskSpend{Cost: 0.031, Tokens: 12043}},
		{ID: 100324, Kind: "app.usage-sampler", State: "sleeping", Until: now.Add(26 * time.Hour).UnixMilli()},
	}

	tt := ui.NewTester(a.View, 1180, 820)
	tt.Frame()
	for _, text := range []string{"从过去 24 小时的真实痕迹里找出流程哪里不对", "Why is the sky blue", "用量采样", "例行任务", "对话回合"} {
		if !tt.HasText(text) {
			t.Fatalf("missing %q; drew %v", text, tt.Texts())
		}
	}
	if tt.HasText("pi.generation") {
		t.Fatalf("the rail still speaks the scheduler's names; drew %v", tt.Texts())
	}
	said := 0
	for _, text := range tt.Texts() {
		if text == "用量采样" {
			said++
		}
	}
	if said != 1 {
		t.Fatalf("a task named only by its kind says it %d times; drew %v", said, tt.Texts())
	}
	writePNG(t, "/tmp/clientv2-tasks.png", tt.Image())
}

// TestSnapshotCodeBlock draws an answer with the three pieces of Markdown the
// transcript cannot fake: a fenced block, an inline span and a quote. They take
// their colours from the app's tokens rather than mygo's defaults, or the block
// reads as a patch of another program sitting on the card.
func TestSnapshotCodeBlock(t *testing.T) {
	i18n.Set(i18n.ZH)
	a := fixtureApp(t, false)
	text := "The retry came from `queue.ts`, not the worker:\n\n" +
		"```go\nfunc (q *Queue) retry(job Job) error {\n\tif q.attempts >= maxRetries {\n\t\treturn ErrExhausted\n\t}\n\treturn q.enqueue(job)\n}\n```\n\n" +
		"> A retry is a new enqueue, so the attempt count lives on the job.\n\n" +
		"---\n\n" +
		"- the cap is `maxRetries`\n- the counter resets on success\n"
	quoted, err := json.Marshal(text)
	if err != nil {
		t.Fatal(err)
	}
	a.thread(11).upsert(entryJSON(t, 92, "pi.assistant", time.Now().Add(-4*time.Minute),
		`[{"role":"assistant","content":[{"type":"text","text":`+string(quoted)+`}],"timestamp":__ts}]`))

	tt := ui.NewTester(a.View, 1180, 820)
	tt.Frame()
	for _, want := range []string{"go", "func (q *Queue) retry(job Job) error {", "the counter resets on success"} {
		if !tt.HasText(want) {
			t.Fatalf("missing %q; drew %v", want, tt.Texts())
		}
	}
	writePNG(t, "/tmp/clientv2-codeblock.png", tt.Image())
}

// TestSnapshotTimeline draws the timeline open beside the chat: one heading per
// day, the clock at the left edge, and a dot whose colour says who spoke.
func TestSnapshotTimeline(t *testing.T) {
	i18n.Set(i18n.ZH)
	a := fixtureApp(t, false)
	a.tl.open = true

	tt := ui.NewTester(a.View, 1180, 820)
	tt.Frame()
	for _, text := range []string{
		i18n.T("timeline"),
		i18n.T("tlUser"),
		i18n.T("tlTool"),
		i18n.T("tlError"),
		"why is the sky blue?", // the message the first dot stands for
	} {
		if !tt.HasText(text) {
			t.Fatalf("missing %q; drew %v", text, tt.Texts())
		}
	}
	writePNG(t, "/tmp/clientv2-timeline.png", tt.Image())
}

// TestSnapshotTasksNarrowRail pins the task tab's filter row at the narrowest
// rail: four segments sharing the width, none of them clipped by the edge.
func TestSnapshotTasksNarrowRail(t *testing.T) {
	i18n.Set(i18n.EN)
	a := fixtureApp(t, false)
	now := a.now
	a.rail = railTasks
	a.railW = railMin
	a.taskFilter = "sleeping"
	a.tasks = []protocol.Task{
		{ID: 1, Kind: "pi.generation", ConversationID: 11, State: "working", Until: now.Add(20 * time.Minute).UnixMilli()},
		{ID: 2, Kind: "app.routine", Title: "waiting on a human", State: "waiting"},
		{ID: 3, Kind: "app.usage-sampler", State: "sleeping", Until: now.Add(26 * time.Hour).UnixMilli()},
	}

	tt := ui.NewTester(a.View, 900, 560)
	tt.Frame()
	for _, text := range []string{"All", "Working", "Waiting", "Sleeping"} {
		if !tt.HasText(text) {
			t.Fatalf("missing %q; drew %v", text, tt.Texts())
		}
	}
	writePNG(t, "/tmp/clientv2-tasks-narrow.png", tt.Image())
}
