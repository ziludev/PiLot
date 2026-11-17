package app

import "testing"

// TestOpacityClampsAndStores checks the transparency setting: a value from the
// preference file or the slider is clamped into a readable range, and the store
// keeps what every window is drawn with. Painting needs a real window, which is
// why the hook is not reached here.
func TestOpacityClampsAndStores(t *testing.T) {
	t.Cleanup(func() { windowOpacity = maxOpacity })

	a := New(Config{})
	a.prefs = prefs{}
	if a.prefs.opacity() != maxOpacity {
		t.Fatalf("a file with no opacity = %v, want 1", a.prefs.opacity())
	}
	a.setOpacity(0.8)
	if a.prefs.Opacity != 0.8 || windowOpacity != 0.8 {
		t.Fatalf("setOpacity(0.8) = %v / %v", a.prefs.Opacity, windowOpacity)
	}
	a.setOpacity(0.1)
	if a.prefs.Opacity != minOpacity || windowOpacity != minOpacity {
		t.Fatalf("setOpacity(0.1) = %v / %v, want the floor %v", a.prefs.Opacity, windowOpacity, minOpacity)
	}
	a.setOpacity(2)
	if a.prefs.Opacity != maxOpacity || windowOpacity != maxOpacity {
		t.Fatalf("setOpacity(2) = %v / %v, want the ceiling %v", a.prefs.Opacity, windowOpacity, maxOpacity)
	}
	// A saved value survives the round trip through the file's field.
	p := prefs{Opacity: 0.85}
	if p.opacity() != 0.85 {
		t.Fatalf("saved 0.85 reads back as %v", p.opacity())
	}
	// A file written before the setting existed has no field at all.
	if (prefs{}).opacity() != maxOpacity {
		t.Fatalf("an old preference file should read as opaque")
	}
}
