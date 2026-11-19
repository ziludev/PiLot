package app

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"pitta/internal/i18n"
	"pitta/internal/protocol"
	"pitta/internal/theme"
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

	a.setSurfaces([]protocol.Surface{
		{SurfaceID: "client:12", ConversationID: 12, Kind: "feishu", Title: "Deploy checklist for the 0.4 train", ProjectID: "workspace"},
		{SurfaceID: "client:11", ConversationID: 11, Kind: "client", Title: "Why is the sky blue", ProjectID: "workspace"},
		{SurfaceID: "client:9", ConversationID: 9, Kind: "other", Title: "Sandbox scratch", ProjectID: "workspace"},
	})
	a.vm.tails[11] = now.Add(-3 * time.Minute)
	a.vm.tails[12] = now.Add(-2 * time.Hour)
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
		if !tt.HasText("⋯ 2 步 · 12s") {
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
			if !hasAnyText(tt, "Enter 发送") {
				t.Fatalf("%s: the composer lost its hint; drew %v", c.name, tt.Texts())
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
