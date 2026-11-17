package app

import (
	"time"

	"github.com/egoist/mygo/ui"

	"pitta/internal/brand"
)

// drawCycle is how long one drawing takes, and drawHold how long the finished
// face rests before it starts again. A logo that redrew without a pause would
// read as a strikethrough; the beat is what says "done, and still busy".
const (
	drawCycle = 1500 * time.Millisecond
	drawHold  = 600 * time.Millisecond
)

// drawnLogo is the thinking state: the app's mark penning itself, the pen running
// the flame's outline while the ink follows it, then the stand and the filled shape
// arriving as the line closes. It stands where a spinner said only "busy".
func drawnLogo(c *ui.Context, size int, ink string) ui.Element {
	// The frame goes inside the box, not beside it: a box and an image returned
	// together are two layout children, so the row took the box's width as a gap
	// as wide as the mark itself.
	box := ui.Box(c).Size(float32(size), float32(size))
	progress := box.Keyframes("draw", []ui.Keyframe{
		{At: 0, Value: 0},
		{At: drawCycle, Value: 1},
	}, ui.KeyframeOptions{
		Repeat:      ui.RepeatForever,
		RepeatDelay: drawHold,
		NonMotion:   true,
	})
	box.Children(func() { brand.DrawIn(c, size, ink, progress) })
	return box
}
