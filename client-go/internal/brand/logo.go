// Package brand draws the app's mark: the flame on its stand, which is the same
// glyph in the app icon, the tray icon and the rail's head. The two paths are the
// authored ones from resources/pitta-logo.svg (the last version in
// work/pitta-logo/type2), so what the client paints is the shipped mark and not a
// re-estimate of it.
//
// The mark moves in three states: a slow breath when nothing is happening, a
// flicker while a turn or a task runs, and one warm beat when an answer lands.
// Frames are computed from the frame number, the way the exported GIFs were, so a
// loop is seamless however the animation is sampled.
package brand

import (
	"fmt"
	"math"
	"strconv"
	"sync"
	"time"

	"github.com/egoist/mygo/ui"
)

// The mark, in the 256-unit box the SVG is authored in.
const (
	flame = "M128 194 C98 194 82 172 89 144 C96 116 116 106 124 82 C129 66 134 56 144 46 " +
		"C152 74 168 102 172 134 C176 164 160 194 128 194 Z"
	hole = "M128 182 C114 182 107 168 111 152 C115 140 123 134 127 118 C131 106 135 98 141 92 " +
		"C146 112 151 128 152 148 C153 168 145 182 128 182 Z"
	stand = "M36 212 Q128 205 220 212"
)

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
	viewBox   = 256
	baseY     = 194.0
	baseX     = 128.0
)

var (
	// flameOutline is the walk the pen takes when the mark draws itself.
	flameOutline = flatten(flame, 24)
	standOutline = flatten(stand, 24)
	penLength    = flameOutline.length + standOutline.length

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

// DrawIn is the mark drawing itself: a pen runs the flame's outline, then the
// stand, and the mark settles into its shape. progress runs 0..1 once.
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
	return ui.Image(c, svg).Size(float32(size), float32(size))
}

// frameMarkup is the whole mark with the flame moved as the mood says. The stand
// stays where it is: it is the floor the flame moves over.
func frameMarkup(mood Mood, step int, ink string) string {
	phase := float64(step) / Frames * 2 * math.Pi
	var sx, sy, roll, lean float64 = 1, 1, 0, 0
	switch mood {
	case MoodThinking:
		// Two out-of-step waves: one cannot make a flicker look like a metronome.
		f1, f2 := math.Sin(phase*2), math.Sin(phase*3+1.1)
		sy = 1 + f1*0.085
		sx = 1 + f2*0.025
		roll = f1 * 2.2
		// A shear moves the tip further than the base, which is what a flame does
		// when air takes it: a scale about the base alone looks like breathing.
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
	move := ""
	if sx != 1 || sy != 1 || roll != 0 || lean != 0 {
		move = fmt.Sprintf(
			` transform="translate(%.1f %.1f) rotate(%.2f) skewX(%.2f) scale(%.4f %.4f) translate(%.1f %.1f)"`,
			baseX, baseY, roll, lean, sx, sy, -baseX, -baseY)
	}
	return `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 256 256">` +
		`<g` + move + `><path fill-rule="evenodd" fill="` + ink + `" d="` + flame + ` ` + hole + `"/></g>` +
		`<path fill="none" stroke="` + ink + `" stroke-width="17" stroke-linecap="round" d="` + stand + `"/>` +
		`</svg>`
}

// drawingMarkup is one frame of the mark drawing itself: the pen follows the ink,
// the stand is drawn next, and the filled shape arrives as the last stroke closes.
func drawingMarkup(p float64, ink string) string {
	drawn := flameOutline.length * clamp01(p/0.78)
	standDrawn := standOutline.length * clamp01((p-0.78)/0.16)
	filled := clamp01((p - 0.9) / 0.1)

	parts := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 256 256">`
	if filled > 0 {
		parts += fmt.Sprintf(`<path fill-rule="evenodd" fill="%s" fill-opacity="%.3f" d="%s %s"/>`,
			ink, filled, flame, hole)
	}
	parts += fmt.Sprintf(
		`<path fill="none" stroke="%s" stroke-width="10" stroke-linecap="round" stroke-dasharray="%.2f" stroke-dashoffset="%.2f" d="%s"/>`,
		ink, flameOutline.length, flameOutline.length-drawn, flame)
	if standDrawn > 0 {
		length := standLength()
		parts += fmt.Sprintf(
			`<path fill="none" stroke="%s" stroke-width="17" stroke-linecap="round" stroke-dasharray="%.2f" stroke-dashoffset="%.2f" d="%s"/>`,
			ink, length, length-standDrawn, stand)
	}
	if p < 0.9 {
		if x, y, ok := penAt(p); ok {
			parts += fmt.Sprintf(`<circle cx="%.2f" cy="%.2f" r="5" fill="%s"/>`, x, y, ink)
		}
	}
	return parts + `</svg>`
}

// penAt is where the pen is for a fraction of the whole walk: the flame's outline
// first, the stand after it.
func penAt(p float64) (float64, float64, bool) {
	total := penLength
	if total <= 0 {
		return 0, 0, false
	}
	distance := clamp01(p) * total
	if distance <= flameOutline.length {
		return flameOutline.at(distance / flameOutline.length)
	}
	rest := (distance - flameOutline.length) / max(standOutline.length, 0.001)
	return standOutline.at(rest)
}

func standLength() float64 { return standOutline.length }

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

type sample struct {
	x, y float64
	run  float64
}

type flattened struct {
	points []sample
	length float64
}

// at is the point a fraction of the way along the walk.
func (o flattened) at(fraction float64) (float64, float64, bool) {
	if len(o.points) == 0 {
		return 0, 0, false
	}
	fraction = clamp01(fraction)
	want := fraction * o.length
	lo, hi := 0, len(o.points)-1
	for lo < hi {
		mid := (lo + hi) / 2
		if o.points[mid].run < want {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	return o.points[lo].x, o.points[lo].y, true
}

// flatten walks a path of absolute M/L/C/Q/Z commands into points. The mark's
// paths are authored by hand, so the walker refuses anything else rather than
// drawing the shape slightly wrong.
func flatten(d string, steps int) flattened {
	var out flattened
	var cur, start [2]float64
	add := func(x, y float64) {
		if n := len(out.points); n > 0 {
			out.length += math.Hypot(x-out.points[n-1].x, y-out.points[n-1].y)
		}
		out.points = append(out.points, sample{x: x, y: y, run: out.length})
	}
	for i := 0; i < len(d); {
		c := d[i]
		if c == ' ' || c == ',' || c == '\n' || c == '\t' {
			i++
			continue
		}
		i++
		args, next, ok := pathArgs(d, i, commandArity(c))
		if !ok {
			panic("brand: bad path argument in " + d)
		}
		i = next
		switch c {
		case 'M':
			cur, start = [2]float64{args[0], args[1]}, [2]float64{args[0], args[1]}
			add(cur[0], cur[1])
		case 'L':
			cur = [2]float64{args[0], args[1]}
			add(cur[0], cur[1])
		case 'C':
			from := cur
			for s := 1; s <= steps; s++ {
				t := float64(s) / float64(steps)
				add(bezier(from[0], args[0], args[2], args[4], t), bezier(from[1], args[1], args[3], args[5], t))
			}
			cur = [2]float64{args[4], args[5]}
		case 'Q':
			from := cur
			for s := 1; s <= steps; s++ {
				t := float64(s) / float64(steps)
				add(quadratic(from[0], args[0], args[2], t), quadratic(from[1], args[1], args[3], t))
			}
			cur = [2]float64{args[2], args[3]}
		case 'Z':
			cur = start
			add(cur[0], cur[1])
		default:
			panic("brand: unsupported path command " + string(c))
		}
	}
	return out
}

func commandArity(c byte) int {
	switch c {
	case 'M', 'L':
		return 2
	case 'C':
		return 6
	case 'Q':
		return 4
	case 'Z':
		return 0
	}
	return -1
}

func pathArgs(d string, i, count int) ([]float64, int, bool) {
	if count < 0 {
		return nil, i, false
	}
	args := make([]float64, 0, count)
	for n := 0; n < count; n++ {
		for i < len(d) && (d[i] == ' ' || d[i] == ',' || d[i] == '\n' || d[i] == '\t') {
			i++
		}
		start := i
		for i < len(d) && (d[i] == '-' || d[i] == '+' || d[i] == '.' || (d[i] >= '0' && d[i] <= '9')) {
			i++
		}
		if i == start {
			return nil, i, false
		}
		v, err := strconv.ParseFloat(d[start:i], 64)
		if err != nil {
			return nil, i, false
		}
		args = append(args, v)
	}
	return args, i, true
}

func bezier(p0, p1, p2, p3, t float64) float64 {
	u := 1 - t
	return u*u*u*p0 + 3*u*u*t*p1 + 3*u*t*t*p2 + t*t*t*p3
}

func quadratic(p0, p1, p2, t float64) float64 {
	u := 1 - t
	return u*u*p0 + 2*u*t*p1 + t*t*p2
}

func max(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}
