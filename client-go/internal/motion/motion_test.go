package motion

import (
	"testing"
	"time"
)

// TestCurvesAreEasings pins the two curves to the shape they stand for: both
// end where they started and finish, and EaseOutCubic is the faster of the two
// early on, since it slows into its target instead of starting slowly.
func TestCurvesAreEasings(t *testing.T) {
	for name, ease := range map[string]func(float32) float32{"inOut": EaseInOutCubic, "out": EaseOutCubic} {
		if got := ease(0); got > 0.01 || got < -0.01 {
			t.Fatalf("%s(0) = %v, want 0", name, got)
		}
		if got := ease(1); got < 0.99 || got > 1.01 {
			t.Fatalf("%s(1) = %v, want 1", name, got)
		}
		last := float32(0)
		for i := 1; i <= 10; i++ {
			at := ease(float32(i) / 10)
			if at < last-0.01 {
				t.Fatalf("%s is not monotone: %v after %v at %v", name, at, last, float32(i)/10)
			}
			last = at
		}
	}
	if EaseOutCubic(0.25) <= EaseInOutCubic(0.25) {
		t.Fatalf("EaseOutCubic(0.25) = %v, want more than EaseInOutCubic's %v: it starts fast",
			EaseOutCubic(0.25), EaseInOutCubic(0.25))
	}
}

// TestTransitionsCarryTheOldNumbers keeps the ports honest: changing a
// duration here means changing motion.ts too, or the clients drift apart.
func TestTransitionsCarryTheOldNumbers(t *testing.T) {
	if Hover().Duration != HoverLift || !Hover().Colors || !Hover().Shadows {
		t.Fatalf("Hover = %+v", Hover())
	}
	if AccordionTransition().Duration != Accordion || !AccordionTransition().Size {
		t.Fatalf("AccordionTransition = %+v", AccordionTransition())
	}
	if PopoverEnter().Duration != Popover || PopoverEnter().ExitDuration != PopoverExit {
		t.Fatalf("PopoverEnter = %+v", PopoverEnter())
	}
	if Lift.Duration != HoverLift || FadeIn().Enter == nil {
		t.Fatalf("Lift = %+v, FadeIn = %+v", Lift, FadeIn())
	}
}

// TestToastComesInFromTheEdge pins the toast slide: 240 px off the right edge
// and faded out on both ends, over 350 ms each way.
func TestToastComesInFromTheEdge(t *testing.T) {
	tr := ToastEnter()
	if tr.Duration != 350*time.Millisecond || tr.ExitDuration != 350*time.Millisecond {
		t.Fatalf("toast slide takes %v in, %v out, want 350 ms each", tr.Duration, tr.ExitDuration)
	}
	if tr.Enter == nil || tr.Exit == nil {
		t.Fatalf("toast slide = %+v, want both ends", tr)
	}
	if tr.Enter.X != 240 || tr.Exit.X != 240 {
		t.Fatalf("toast slides from x=%v in, %v out, want 240", tr.Enter.X, tr.Exit.X)
	}
	if tr.Enter.Opacity != 0 || tr.Exit.Opacity != 0 {
		t.Fatalf("toast is %v opaque in, %v out, want 0", tr.Enter.Opacity, tr.Exit.Opacity)
	}
	if tr.Enter.Y != 0 || tr.Enter.Scale != 0 {
		t.Fatalf("toast moves on x only: %+v", tr.Enter)
	}
}

// TestPanelNumbers pins the side panel's motion: its content comes in 24 px from
// the right over 200 ms.
func TestPanelNumbers(t *testing.T) {
	in := PanelEnter()
	if in.Duration != 200*time.Millisecond || in.Enter == nil || in.Exit != nil {
		t.Fatalf("panel enter = %+v, want 200 ms in and nothing out", in)
	}
	if in.Enter.X != 24 || in.Enter.Opacity != 0 || in.Enter.Y != 0 {
		t.Fatalf("panel comes in as %+v, want 24 px from the right, faded", in.Enter)
	}
}

// TestLifeOutlastsItsBarrier holds the one number that is about two others: a
// toast has to outlive both of its slides, or it would start leaving while it is
// still arriving.
func TestLifeOutlastsItsBarrier(t *testing.T) {
	if ToastLife < 2*ToastSlideTime {
		t.Fatalf("ToastLife = %v, want at least two slides (%v)", ToastLife, 2*ToastSlideTime)
	}
	if PulseLow <= 0 || PulseLow >= 1 {
		t.Fatalf("PulseLow = %v, want a breath between invisible and solid", PulseLow)
	}
	if Pulse < 2*Tabs {
		t.Fatalf("Pulse = %v, want a breath slower than a tab slide (%v) so it reads as one", Pulse, Tabs)
	}
}
