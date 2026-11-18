// Package brand draws the app's mark: the four modules and the arrow on their
// stand, which is the same glyph in the app icon, the tray icon and the rail's
// head. The paths are the authored ones from resources/pilot-mark.svg (the mono
// lockup of the PiLot brand), so what the client paints is the shipped mark and
// not a re-estimate of it.
//
// The mark moves in three states: a slow breath when nothing is happening, a
// flicker while a turn or a task runs, and one warm beat when an answer lands.
// Frames are computed from the frame number, the way the exported GIFs were, so a
// loop is seamless however the animation is sampled.
package brand

import (
	_ "embed"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo/ui"
)

// markSVG is the mark in the 350x350 box it is authored in, with the arrow cut
// out of the modules by a mask. __INK__ is the colour the window asks for and the
// __OP_*__ slots are the modules' opacities, so one file serves every frame.
//
//go:embed mark.svg
var markSVG string

// Mood is what the mark is doing. The names follow the exported state set:
// idle breathes, working flickers, done takes one beat.
type Mood int

const (
	MoodIdle Mood = iota
	MoodThinking
	MoodHappy
)

const (
	// Frames and FrameTime are the exported GIFs' own: 26 frames at 16 fps.
	Frames    = 26
	frameTime = 61 * time.Millisecond // 16 fps, as the exported GIFs
	// The mark is authored in a 350x288 box and padded to a square one, so a
	// square box draws it undistorted; baseX and baseY are the middle it moves
	// about.
	baseX = 175.0
	baseY = 144.0
)

// modules are the shapes the drawing state reveals, in the order it reveals
// them: the four tiles, then the stem the arrow rides on.
var modules = []string{"BLUE", "CORAL", "PURPLE", "YELLOW", "STEM"}

var (
	framesMu sync.Mutex
	frames   = map[frameKey]*ui.SVG{}
	drawings = map[drawKey]*ui.SVG{}
	drawMu   sync.Mutex
)

type frameKey struct {
	mood Mood
	step int
	ink  string
}

type drawKey struct {
	progress int
	ink      string
}

// Loop is how long one pass of the animation takes.
func Loop() time.Duration { return Frames * frameTime }

// Ink is a colour as the SVG wants it.
func Ink(c ui.Color) string {
	return fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B)
}

// Draw runs the mood's loop: the caller places the element and the animation
// clock advances it, so the mark's frames come from the same keyframes as every
// other moving thing in the window.
func Draw(c *ui.Context, size int, mood Mood, ink string) ui.Element {
	box := ui.Box(c).Size(float32(size), float32(size))
	value := box.Keyframes("mark", []ui.Keyframe{
		{At: 0, Value: 0},
		{At: Loop(), Value: 1},
	}, ui.KeyframeOptions{Repeat: ui.RepeatForever, NonMotion: true})
	// The frame is the box's child, not a sibling: a row that took both would
	// lay out the box's width as well, a gap as wide as the mark itself.
	box.Children(func() {
		Frame(c, size, mood, ink, int(float64(value)*Frames))
	})
	return box
}

// View draws the mark still, at the first frame of its mood.
func View(c *ui.Context, size int, mood Mood, ink string) ui.Element {
	return Frame(c, size, mood, ink, 0)
}

// Frame draws one frame of the mood's loop.
func Frame(c *ui.Context, size int, mood Mood, ink string, frame int) ui.Element {
	step := frame % Frames
	if step < 0 {
		step += Frames
	}
	key := frameKey{mood: mood, step: step, ink: ink}
	framesMu.Lock()
	svg, ok := frames[key]
	framesMu.Unlock()
	if !ok {
		svg = ui.MustParseSVG([]byte(frameMarkup(mood, step, ink)))
		framesMu.Lock()
		frames[key] = svg
		framesMu.Unlock()
	}
	return ui.Image(c, svg).Size(float32(size), float32(size)).Label(MarkLabel)
}

// MarkLabel names the mark's image for callers that need its box: an SVG
// carries no text to find, and the rail header's geometry is what they measure.
const MarkLabel = "mark"

// DrawIn is the mark assembling itself: the tiles arrive one after another, the
// stem last, and the arrow is cut through them as they land. progress runs 0..1
// once.
func DrawIn(c *ui.Context, size int, ink string, progress float32) ui.Element {
	// A tenth of a percent is finer than a 20px mark can show, and the element is
	// rebuilt every frame, so the parse is cached per step.
	step := int(clamp01(float64(progress)) * 1000)
	key := drawKey{progress: step, ink: ink}
	drawMu.Lock()
	svg, ok := drawings[key]
	drawMu.Unlock()
	if !ok {
		svg = ui.MustParseSVG([]byte(drawingMarkup(float64(step)/1000, ink)))
		drawMu.Lock()
		drawings[key] = svg
		drawMu.Unlock()
	}
	return ui.Image(c, svg).Size(float32(size), float32(size)).Label(MarkLabel)
}

// frameMarkup is the whole mark, moved as the mood says. The modules keep their
// shape: what moves is the group they hang in, the way a logo breathes.
func frameMarkup(mood Mood, step int, ink string) string {
	return strings.NewReplacer(append([]string{
		"__INK__", ink,
		"__MOVE__", moodMove(mood, step),
	}, opacities(1)...)...).Replace(markSVG)
}

// moodMove is the transform of one frame, about the mark's middle.
func moodMove(mood Mood, step int) string {
	phase := float64(step) / Frames * 2 * math.Pi
	var sx, sy, roll, lean float64 = 1, 1, 0, 0
	switch mood {
	case MoodThinking:
		// Two out-of-step waves: one cannot make a flicker look like a metronome.
		f1, f2 := math.Sin(phase*2), math.Sin(phase*3+1.1)
		sy = 1 + f1*0.085
		sx = 1 + f2*0.025
		roll = f1 * 2.2
		// A shear moves the far edge further than the near one, which is what a
		// mark does when the work takes it: a scale about the middle alone looks
		// like breathing, which is the idle state's own move.
		lean = f2 * 5
	case MoodHappy:
		// One beat near the top of the loop, then still.
		if p := float64(step) / Frames; p < 0.4 {
			beat := math.Sin(p / 0.4 * math.Pi)
			sy = 1 + beat*0.08
			sx = 1 + beat*0.05
		}
	default:
		sy = 1 + math.Sin(phase)*0.022
	}
	if sx == 1 && sy == 1 && roll == 0 && lean == 0 {
		return "translate(0 0)"
	}
	return fmt.Sprintf(
		`translate(%.1f %.1f) rotate(%.2f) skewX(%.2f) scale(%.4f %.4f) translate(%.1f %.1f)`,
		baseX, baseY, roll, lean, sx, sy, -baseX, -baseY)
}

// opacities fills the __OP_*__ slots for every module with one value.
func opacities(v float64) []string {
	out := make([]string, 0, len(modules)*2)
	for _, name := range modules {
		out = append(out, "__OP_"+name+"__", strconv.FormatFloat(v, 'f', 3, 64))
	}
	return out
}

// drawingMarkup is one frame of the mark assembling itself: each module fades in
// a tenth of the way behind the one before it, so the four tiles land before the
// stem they sit on.
func drawingMarkup(p float64, ink string) string {
	var ops []float64
	for i := range modules {
		start := 0.12 * float64(i)
		ops = append(ops, clamp01((p-start)/0.4))
	}
	replacements := []string{"__INK__", ink, "__MOVE__", "translate(0 0)"}
	for i, name := range modules {
		replacements = append(replacements, "__OP_"+name+"__", strconv.FormatFloat(ops[i], 'f', 3, 64))
	}
	return strings.NewReplacer(replacements...).Replace(markSVG)
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
