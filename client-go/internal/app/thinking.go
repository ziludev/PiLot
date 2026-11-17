package app

import (
	"time"

	"github.com/egoist/mygo/ui"

	"pitta/internal/avatar"
)

// drawCycle is how long one drawing takes, and drawHold how long the finished
// face rests before it starts again. A logo that redrew without a pause would
// read as a strikethrough; the beat is what says "done, and still busy".
const (
	drawCycle = 1500 * time.Millisecond
	drawHold  = 600 * time.Millisecond
)

// drawnLogo is the bot's thinking state: its own logo penning itself, the stroke
// running around the body while the pen follows the ink, then the face arriving
// as the line closes. It stands where a spinner said only "busy".
func drawnLogo(c *ui.Context, size int, tone avatar.Tone) ui.Element {
	box := ui.Box(c).Size(float32(size), float32(size))
	progress := box.Keyframes("draw", []ui.Keyframe{
		{At: 0, Value: 0},
		{At: drawCycle, Value: 1},
	}, ui.KeyframeOptions{
		Repeat:      ui.RepeatForever,
		RepeatDelay: drawHold,
		NonMotion:   true,
	})
	return avatar.Draw(c, size, tone, progress)
}
