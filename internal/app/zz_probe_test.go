package app

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestProbeComposerWidth(t *testing.T) {
	a := New(Config{})
	tt := ui.NewTester(a.View, 1200, 800)
	tt.Frame()
	if r, ok := tt.Find("Send"); ok {
		t.Logf("Send rect %+v", r)
	} else {
		t.Logf("Send not found; texts=%q", tt.Texts())
	}
	if r, ok := tt.Find("Say something"); ok {
		t.Logf("placeholder rect %+v", r)
	}
}
