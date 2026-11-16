// Package avatar draws the bot's face, the React client's BotAvatar: Pie, the
// scalloped pie from client/src/lib/avatar.ts, as one SVG in that definition's
// 100-unit box scaled to the size asked for.
//
// The client mounts the animated renderer (volume, blinking, gaze); a static
// face carries the mood in the eyes instead: full pills when idle, squinted
// pills looking up and to the right when thinking, arcs when happy.
package avatar

import (
	"fmt"
	"sync"

	"github.com/egoist/mygo/ui"
)

// Mood is what the face shows, as AvatarMood in client/src/lib/avatar.ts.
type Mood int

const (
	MoodIdle Mood = iota
	MoodThinking
	MoodHappy
)

// Tone is the body and eye colour, as AvatarTone: a delegated subagent gets its
// own body colour so background work is visible at a glance, and a short teal
// beat marks the moment it finished. Violet eyes read muddy in brown, so each
// tone brings its own ink.
type Tone struct {
	Color    ui.Color
	EyeColor ui.Color
}

// Tones are AVATAR_TONES: amber is the bot itself.
var Tones = struct {
	Idle      Tone
	Delegated Tone
	Done      Tone
}{
	Idle:      Tone{Color: ui.Hex("#E8A33D"), EyeColor: ui.Hex("#2A1A0C")},
	Delegated: Tone{Color: ui.Hex("#6C7BF5"), EyeColor: ui.Hex("#101233")},
	Done:      Tone{Color: ui.Hex("#14B3A5"), EyeColor: ui.Hex("#04211F")},
}

// body is PIE.body in client/src/lib/avatar.ts, in the definition's 100-unit box.
const body = "M50 11 C55 11 57 15 60 16 C64 17 66 13 70 16 C75 19 74 24 78 27 " +
	"C82 30 87 28 89 33 C91 38 86 42 86 46 C86 51 91 54 88 59 C85 64 80 63 78 68 " +
	"C76 73 81 77 77 82 C73 87 68 83 63 84 C58 85 57 90 51 90 C45 90 43 85 38 84 " +
	"C33 83 29 88 24 84 C19 80 23 75 19 71 C15 67 10 68 9 62 C8 56 14 53 14 48 " +
	"C14 43 9 39 12 34 C15 29 20 31 23 27 C26 23 22 18 27 14 C32 10 37 16 42 15 " +
	"C46 14 46 11 50 11 Z"

// The eye box of PIE.eyes: EYE_W×EYE_H from open-bot-avatar's validate.ts,
// times the definition's eye scale, at the definition's centre and spacing.
const (
	eyeW       = 4 * 1.12 // 4.48
	eyeH       = 11 * 1.12
	eyeCy      = 49
	eyeSpacing = 16
	eyeHalfW   = eyeW / 2
	eyeHalfH   = eyeH / 2
	eyeLeft    = 50 - eyeSpacing/2
	eyeRight   = 50 + eyeSpacing/2
)

// A happy eye is an arc, not a pill: HAPPY_ARC in validate.ts.
const (
	arcHalfWidth = 0.72
	arcBase      = 0.1
	arcPeak      = 0.42
	arcStroke    = 0.6
)

// View draws the face at size×size DIPs, tinted by tone.
func View(c *ui.Context, size int, mood Mood, tone Tone) ui.Element {
	return ui.Image(c, face(mood, tone)).Size(float32(size), float32(size))
}

var faces = struct {
	sync.Mutex
	byMoodTone map[faceKey]*ui.SVG
}{byMoodTone: map[faceKey]*ui.SVG{}}

type faceKey struct {
	mood Mood
	tone Tone
}

// face is the parsed SVG of one face, cached: a view rebuilds its elements
// every frame, and parsing a path per frame would be wasted work. The SVG is
// the same at every size, which the element's Size scales.
func face(mood Mood, tone Tone) *ui.SVG {
	key := faceKey{mood, tone}
	faces.Lock()
	defer faces.Unlock()
	s, ok := faces.byMoodTone[key]
	if !ok {
		s = ui.MustParseSVG([]byte(markup(mood, tone)))
		faces.byMoodTone[key] = s
	}
	return s
}

// markup is the whole face: the body and both eyes, in the definition's box.
func markup(mood Mood, tone Tone) string {
	ink := hex(tone.EyeColor)
	// Thinking rolls the body about its base, as the animated face's -5° roll
	// does: one still frame of it.
	tilt := ""
	if mood == MoodThinking {
		tilt = ` transform="rotate(-4 50 90)"`
	}
	return `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100"><g` + tilt + `>` +
		`<path d="` + body + `" fill="` + hex(tone.Color) + `"/>` + eyes(mood, ink) +
		`</g></svg>`
}

func eyes(mood Mood, ink string) string {
	switch mood {
	case MoodHappy:
		return arc(eyeLeft, ink) + arc(eyeRight, ink)
	case MoodThinking:
		// Thinking squints and looks up and to the right. A still face has none of
		// the motion to read that from, so the squint and the look are deeper than
		// any one animated frame shows.
		return pill(eyeLeft+1.4, 45.4, eyeHalfW, eyeHalfH*0.54, ink) +
			pill(eyeRight+1.4, 45.4, eyeHalfW, eyeHalfH*0.48, ink)
	default:
		return pill(eyeLeft, eyeCy, eyeHalfW, eyeHalfH, ink) +
			pill(eyeRight, eyeCy, eyeHalfW, eyeHalfH, ink)
	}
}

func pill(cx, cy, rx, ry float64, ink string) string {
	return fmt.Sprintf(`<ellipse cx="%.2f" cy="%.2f" rx="%.2f" ry="%.2f" fill="%s"/>`,
		cx, cy, rx, ry, ink)
}

// arc is a happy eye: a stroke from the eye's corners up to a peak, as
// happyArc in open-bot-avatar's render.ts.
func arc(cx float64, ink string) string {
	hw := eyeW * arcHalfWidth
	base := eyeCy + eyeH*arcBase
	peak := eyeCy - eyeH*arcPeak
	return fmt.Sprintf(`<path d="M%.2f %.2f Q%.2f %.2f %.2f %.2f" fill="none" stroke="%s" stroke-width="%.2f" stroke-linecap="round"/>`,
		cx-hw, base, cx, peak, cx+hw, base, ink, eyeW*arcStroke)
}

// hex is an SVG colour for c, which draws with the same sRGB value.
func hex(c ui.Color) string {
	return fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B)
}
