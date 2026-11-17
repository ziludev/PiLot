package avatar

import (
	"fmt"
	"math"
	"strconv"
	"sync"

	"github.com/egoist/mygo/ui"
)

// Draw is the bot while it is thinking: Pie penning itself, rather than a still
// face beside a spinner. The stroke runs around the body up to p (0 to 1 of its
// length), with the pen at the point the ink has reached; the fill and the eyes
// arrive over the last fifth, so the finished drawing is the face that thought
// and then stopped moving.
//
// p is quantized, because every step is a parsed SVG and the element is rebuilt
// every frame: sixty steps over a one-and-a-half second drawing read as smooth
// at 18 pixels, and a step the map already holds costs nothing.
func Draw(c *ui.Context, size int, tone Tone, p float32) ui.Element {
	if p >= 1 {
		return View(c, size, MoodThinking, tone)
	}
	return ui.Image(c, drawFace(tone, quantize(p))).Size(float32(size), float32(size))
}

// drawSteps is how many parsed frames one full drawing passes through.
const drawSteps = 60

func quantize(p float32) int {
	if p <= 0 {
		return 0
	}
	step := int(p*drawSteps + 0.5)
	if step > drawSteps-1 {
		step = drawSteps - 1
	}
	return step
}

var drawings = struct {
	sync.Mutex
	byKey map[drawKey]*ui.SVG
}{byKey: map[drawKey]*ui.SVG{}}

type drawKey struct {
	tone Tone
	step int
}

func drawFace(tone Tone, step int) *ui.SVG {
	key := drawKey{tone, step}
	drawings.Lock()
	defer drawings.Unlock()
	if s, ok := drawings.byKey[key]; ok {
		return s
	}
	s := ui.MustParseSVG([]byte(drawMarkup(tone, float64(step)/drawSteps)))
	drawings.byKey[key] = s
	return s
}

// drawMarkup is one frame: the stroke up to p, the pen where it stopped, and the
// fill with the eyes fading in behind the closing line.
func drawMarkup(tone Tone, p float64) string {
	shell := hex(tone.Color)
	ink := hex(tone.EyeColor)
	face := clamp01((p - 0.8) / 0.2)
	pen := ""
	if x, y, ok := outline.at(p); ok {
		pen = fmt.Sprintf(`<circle cx="%.2f" cy="%.2f" r="3.4" fill="%s"/>`, x, y, ink)
	}
	return `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100">` +
		fmt.Sprintf(`<path d="%s" fill="%s" fill-opacity="%.3f"/>`, body, shell, face) +
		fmt.Sprintf(`<path d="%s" fill="none" stroke="%s" stroke-width="5" stroke-linecap="round" `+
			`stroke-linejoin="round" stroke-dasharray="%.2f" stroke-dashoffset="%.2f"/>`,
			body, shell, outline.length, outline.length*(1-p)) +
		fmt.Sprintf(`<g opacity="%.3f">%s</g>`, face, eyes(MoodThinking, ink)) +
		pen + `</svg>`
}

func clamp01(v float64) float64 {
	switch {
	case v < 0:
		return 0
	case v > 1:
		return 1
	}
	return v
}

// sample is one point on the flattened outline, with how far along it sits.
type sample struct {
	x, y float64
	run  float64
}

// flattened is a path's outline as a polyline: its length, and the point at any
// fraction of it, which is what a stroke drawn up to a fraction of a dash array
// and the pen beside it both need.
type flattened struct {
	points []sample
	length float64
}

// at returns the point p of the way around, and whether there is one.
func (f flattened) at(p float64) (float64, float64, bool) {
	if len(f.points) < 2 || f.length <= 0 {
		return 0, 0, false
	}
	want := clamp01(p) * f.length
	for i := 1; i < len(f.points); i++ {
		next := f.points[i]
		if next.run < want {
			continue
		}
		prev := f.points[i-1]
		span := next.run - prev.run
		if span <= 0 {
			return next.x, next.y, true
		}
		t := (want - prev.run) / span
		return prev.x + (next.x-prev.x)*t, prev.y + (next.y-prev.y)*t, true
	}
	last := f.points[len(f.points)-1]
	return last.x, last.y, true
}

// outline is the body flattened once: the same shape the face fills, walked as a
// line so a frame can draw part of it.
var outline = flatten(body, 24)

// flatten walks the M, L, C and Z commands the body uses, sampling each curve,
// and panics on anything else: the path is a constant, so an unhandled command is
// a mistake to find in a test rather than a face drawn wrong.
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
		switch c {
		case ' ', ',', '\n', '\t':
			i++
			continue
		}
		i++
		args, next, ok := pathArgs(d, i, commandArity(c))
		if !ok {
			panic("avatar: bad path argument in " + d)
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
				add(cubic(from[0], args[0], args[2], args[4], t), cubic(from[1], args[1], args[3], args[5], t))
			}
			cur = [2]float64{args[4], args[5]}
		case 'Z':
			cur = start
			add(cur[0], cur[1])
		default:
			panic("avatar: unsupported path command " + string(c))
		}
	}
	return out
}

// commandArity is how many numbers each supported command carries. A path may
// pack them against the command, as the body's does with "M50 11".
func commandArity(c byte) int {
	switch c {
	case 'M', 'L':
		return 2
	case 'C':
		return 6
	case 'Z':
		return 0
	}
	return -1
}

// pathArgs reads count numbers from d from i, skipping the separators a path may
// use between them, and says where it stopped.
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

// cubic is one axis of a cubic Bézier at t.
func cubic(p0, p1, p2, p3, t float64) float64 {
	u := 1 - t
	return u*u*u*p0 + 3*u*u*t*p1 + 3*u*t*t*p2 + t*t*t*p3
}
