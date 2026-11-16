package icons

import "testing"

// TestEveryIconParses guards the embedded SVGs: a typo would otherwise only
// show as a blank spot at runtime.
func TestEveryIconParses(t *testing.T) {
	for _, icon := range []struct {
		name string
		svg  interface {
			Size() (float32, float32)
		}
	}{
		{"CornerDownRight", CornerDownRight}, {"Monitor", Monitor}, {"MessagesSquare", MessagesSquare},
		{"Languages", Languages}, {"Plus", Plus}, {"Archive", Archive},
		{"RotateCw", RotateCw}, {"Database", Database}, {"Search", Search},
		{"Pin", Pin}, {"Check", Check}, {"X", X}, {"Copy", Copy},
		{"Play", Play}, {"Square", Square}, {"Folder", Folder},
		{"ChevronDown", ChevronDown}, {"ChevronRight", ChevronRight},
		{"Paperclip", Paperclip}, {"Send", Send},
		{"PanelRight", PanelRight}, {"Brain", Brain}, {"ShieldCheck", ShieldCheck},
		{"GitCommit", GitCommit}, {"GitCompare", GitCompare}, {"Trash", Trash},
		{"Wand", Wand}, {"MessagePlus", MessagePlus}, {"Signal", Signal},
		{"ChevronLeft", ChevronLeft}, {"ExternalLink", ExternalLink},
		{"FileIcon", FileIcon}, {"Clock", Clock}, {"Star", Star},
	} {
		if w, h := icon.svg.Size(); w != 24 || h != 24 {
			t.Fatalf("%s: size %vx%v, want 24x24", icon.name, w, h)
		}
	}
}
