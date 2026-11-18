// Package motion holds the earlier web client's motion numbers, so the two clients
// move alike: src/lib/motion.ts names each curve after the Motion Prompt spec
// it came from, and the values here are those numbers.
package motion

import (
	"time"

	"github.com/egoist/mygo/ui"
)

// The two curves the earlier web client uses, as CSS cubic-bezier tuples there.
var (
	// EaseInOutCubic is cubic-bezier(0.65, 0, 0.35, 1).
	EaseInOutCubic = ui.CubicBezier(0.65, 0, 0.35, 1)
	// EaseOutCubic is cubic-bezier(0.33, 1, 0.68, 1).
	EaseOutCubic = ui.CubicBezier(0.33, 1, 0.68, 1)
)

// Durations, in the order motion.ts declares them.
const (
	// Accordion is how long a step takes to open or fold (#052).
	Accordion = 350 * time.Millisecond
	// Popover is how long a popover takes to slide in from its anchor (#055).
	Popover = 280 * time.Millisecond
	// PopoverExit is the popover's 150 ms fade out.
	PopoverExit = 150 * time.Millisecond
	// Tabs is how long a tab's underline takes to move, and its content to
	// rise into place over 200 ms after a 100 ms hand-over (#057).
	Tabs = 250 * time.Millisecond
	// HoverLift is how long a row takes to lift and cast its shadow (#081).
	HoverLift = 300 * time.Millisecond
	// EmptyState is the 350 ms fade-and-rise a screen that has nothing to
	// show comes in with (#072).
	EmptyState = 350 * time.Millisecond
	// ToastSlideTime is how long a toast takes to come in from the right, and
	// to go back out on the same path.
	ToastSlideTime = 350 * time.Millisecond
	// PanelSlide is how long a side panel's content takes to come in from the
	// right when a tab swaps it: the faster half of #057, the content rather
	// than the tab strip.
	PanelSlide = 200 * time.Millisecond
)

// Offsets, in DIPs.
const (
	// HoverRise is how far a hovered row lifts.
	HoverRise = 2
	// PopoverShift is how far a popover slides in.
	PopoverShift = 6
	// EmptyRise is how far an empty state rises as it fades in.
	EmptyRise = 8
	// ContentRise is how far a swapped-in panel rises.
	ContentRise = 8
	// ToastSlide is how far a toast comes in from, off the right edge.
	ToastSlide = 240
	// PanelShift is how far a swapped-in panel's content starts to the right.
	PanelShift = 24
)

// Offset and lifetime numbers that tie two motions together.
const (
	// ToastLife is how long a toast stays before it slides out: 1.8 s, which
	// is also how long its drain bar takes to empty.
	ToastLife = 1800 * time.Millisecond
	// Pulse is how long a running step's dot takes to breathe in and out once,
	// and PulseLow how faint it gets at the bottom of that breath. The dot is
	// the one mark that says a step is still going, so it fades between two
	// tones of its own colour rather than moving and dragging the row with it.
	Pulse    = 1400 * time.Millisecond
	PulseLow = 0.35
)

// Lift is the animation a hovered row moves by.
var Lift = ui.Animation{Duration: HoverLift, Ease: EaseOutCubic}

// Hover is the transition rows, buttons and cards carry so their background,
// border and shadow follow the pointer instead of jumping: the earlier client names the same
// properties in transition-[background-color,box-shadow].
func Hover() ui.ElementTransition {
	return ui.ElementTransition{Duration: HoverLift, Ease: EaseOutCubic, Colors: true, Shadows: true}
}

// RowLeave is how an archived chat folds out of the rail: it collapses and
// fades over this long. The fold is driven by AnimateTo, not by Exit — a
// ui.List row is dropped the moment the data says so, so there is nothing left
// to run an exit motion on (v0.10.0 still behaves this way).
const RowLeave = 220 * time.Millisecond

// Fold is the animation a rail row closes with when its chat is archived.
var Fold = ui.Animation{Duration: RowLeave, Ease: EaseInOutCubic}

// Accordion is the transition an opening step carries: its content grows and
// folds over 350 ms instead of appearing.
func AccordionTransition() ui.ElementTransition {
	return ui.ElementTransition{
		Duration: Accordion,
		Ease:     EaseInOutCubic,
		Size:     true,
		Enter:    &ui.Motion{Collapse: true},
		Exit:     &ui.Motion{Collapse: true},
	}
}

// PopoverEnter is how a popover comes in: 6 px from its anchor over 280 ms.
func PopoverEnter() ui.ElementTransition {
	return ui.ElementTransition{
		Duration:     Popover,
		Ease:         EaseOutCubic,
		ExitDuration: PopoverExit,
		ExitEase:     ui.Linear,
		Enter:        &ui.Motion{Y: PopoverShift, Opacity: 0},
		Exit:         &ui.Motion{Opacity: 0},
	}
}

// FadeIn is how content that replaces other content comes in: it rises 8 px
// while it fades, as the tab panels do.
func FadeIn() ui.ElementTransition {
	return ui.ElementTransition{
		Duration: Tabs,
		Ease:     EaseOutCubic,
		Enter:    &ui.Motion{Y: ContentRise, Opacity: 0},
	}
}

// TabSlide is how fast the underline under the side panel's tabs moves from
// the tab that was picked to the one just picked: the strip's own duration, in
// and out, so it reads as one marker travelling rather than a new one appearing.
var TabSlide = ui.Animation{Duration: Tabs, Ease: EaseInOutCubic}

// PanelEnter is how a side panel's content comes in when a tab swaps it: 24 px
// from the right, faded, over 200 ms. Enter only: the content a switch replaces
// is gone by the time the new one draws, so there is nothing to animate out.
//
// It cannot be the panel opening from the split, though it reads the same: the
// split and the panel are born on the same frame when the column opens, and a
// transition only plays on a node whose parent was already there. Opening the
// column therefore still cuts; see docs/ui-motion-todo.md.
func PanelEnter() ui.ElementTransition {
	return ui.ElementTransition{
		Duration: PanelSlide,
		Ease:     EaseOutCubic,
		Enter:    &ui.Motion{X: PanelShift, Opacity: 0},
	}
}

// ToastEnter is how a toast arrives and leaves: it starts 240 px past the
// right edge, faded out, and settles in over 350 ms; it leaves by the same path
// over the same time, as sonner's toasts do in the earlier client.
func ToastEnter() ui.ElementTransition {
	return ui.ElementTransition{
		Duration:     ToastSlideTime,
		Ease:         EaseOutCubic,
		ExitDuration: ToastSlideTime,
		ExitEase:     EaseInOutCubic,
		Enter:        &ui.Motion{X: ToastSlide, Opacity: 0},
		Exit:         &ui.Motion{X: ToastSlide, Opacity: 0},
	}
}
