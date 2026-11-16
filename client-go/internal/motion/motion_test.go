package motion

import "testing"

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

// TestTransitionsCarryTheReactNumbers keeps the ports honest: changing a
// duration here means changing motion.ts too, or the clients drift apart.
func TestTransitionsCarryTheReactNumbers(t *testing.T) {
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
