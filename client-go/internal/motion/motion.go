// Package motion holds the React client's motion numbers, so the two clients
// move alike: src/lib/motion.ts names each curve after the Motion Prompt spec
// it came from, and the values here are those numbers.
package motion

import (
	"time"

	"github.com/egoist/mygo/ui"
)

// The two curves the React client uses, as CSS cubic-bezier tuples there.
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
)

// Lift is the animation a hovered row moves by.
var Lift = ui.Animation{Duration: HoverLift, Ease: EaseOutCubic}

// Hover is the transition rows, buttons and cards carry so their background,
// border and shadow follow the pointer instead of jumping: React names the same
// properties in transition-[background-color,box-shadow].
func Hover() ui.ElementTransition {
	return ui.ElementTransition{Duration: HoverLift, Ease: EaseOutCubic, Colors: true, Shadows: true}
}

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
