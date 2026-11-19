package icons

import "github.com/egoist/mygo/ui"

// The window's icons, drawn as Lucide does: 24px grid, 2px strokes,
// currentColor so ui.Icon follows the theme. Only the ones the P0 layout draws
// live here.
var (
	Paperclip     = svg(`<path d="m16 6-8.414 8.586a2 2 0 0 0 2.829 2.829l8.414-8.586a4 4 0 1 0-5.657-5.657l-8.379 8.551a6 6 0 1 0 8.485 8.485l8.379-8.551"/>`)
	Send          = svg(`<path d="M14.536 21.686a.5.5 0 0 0 .937-.024l6.5-19a.496.496 0 0 0-.635-.635l-19 6.5a.5.5 0 0 0-.024.937l7.93 3.18a2 2 0 0 1 1.112 1.11z"/><path d="m21.854 2.147-10.94 10.939"/>`)
	Square        = svg(`<rect width="14" height="14" x="5" y="5" rx="2"/>`)
	ChevronDown   = svg(`<path d="m6 9 6 6 6-6"/>`)
	ChevronRight  = svg(`<path d="m9 18 6-6-6-6"/>`)
	Check         = svg(`<path d="M20 6 9 17l-5-5"/>`)
	Plus          = svg(`<path d="M5 12h14"/><path d="M12 5v14"/>`)
	Search        = svg(`<circle cx="11" cy="11" r="8"/><path d="m21 21-4.3-4.3"/>`)
	X             = svg(`<path d="M18 6 6 18"/><path d="m6 6 12 12"/>`)
	MessagePlus   = svg(`<path d="M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2z"/><path d="M12 7v6"/><path d="M9 10h6"/>`)
	ShieldAlert   = svg(`<path d="M20 13c0 5-3.5 7.5-7.66 8.95a1 1 0 0 1-.67-.01C7.5 20.5 4 18 4 13V6a1 1 0 0 1 1-1c2 0 4.5-1.2 6.24-2.72a1.17 1.17 0 0 1 1.52 0C14.51 3.81 17 5 19 5a1 1 0 0 1 1 1z"/><path d="M12 8v4"/><path d="M12 16h.01"/>`)
	AlertTriangle = svg(`<path d="m21.73 18-8-14a2 2 0 0 0-3.48 0l-8 14A2 2 0 0 0 4 21h16a2 2 0 0 0 1.73-3"/><path d="M12 9v4"/><path d="M12 17h.01"/>`)
	Moon          = svg(`<path d="M12 3a6 6 0 0 0 9 9 9 9 0 1 1-9-9Z"/>`)
	Sun           = svg(`<circle cx="12" cy="12" r="4"/><path d="M12 2v2"/><path d="M12 20v2"/><path d="m4.93 4.93 1.41 1.41"/><path d="m17.66 17.66 1.41 1.41"/><path d="M2 12h2"/><path d="M20 12h2"/><path d="m6.34 17.66-1.41 1.41"/><path d="m19.07 4.93-1.41 1.41"/>`)
	// Folder is a project: the drawer's own mark, and the rail's button to it.
	Folder = svg(`<path d="M20 20a2 2 0 0 0 2-2V8a2 2 0 0 0-2-2h-7.9a2 2 0 0 1-1.69-.9L9.6 3.9A2 2 0 0 0 7.93 3H4a2 2 0 0 0-2 2v13a2 2 0 0 0 2 2Z"/>`)
	Clock  = svg(`<circle cx="12" cy="12" r="10"/><polyline points="12 6 12 12 16 14"/>`)
	// Calendar is a month: two hooks on top, a header rule, and the weeks below
	// it, the shape a wall calendar has had for a century.
	Calendar  = svg(`<path d="M8 2v4"/><path d="M16 2v4"/><rect width="18" height="18" x="3" y="4" rx="2"/><path d="M3 10h18"/><path d="M3 14h18"/><path d="M3 18h18"/>`)
	RefreshCw = svg(`<path d="M3 12a9 9 0 0 1 9-9 9.75 9.75 0 0 1 6.74 2.74L21 8"/><path d="M21 3v5h-5"/><path d="M21 12a9 9 0 0 1-9 9 9.75 9.75 0 0 1-6.74-2.74L3 16"/><path d="M8 16H3v5"/>`)
	// Database is the memory drawer: what has been stored, in the shape every
	// stack of disks has drawn since the first one.
	Database = svg(`<ellipse cx="12" cy="5" rx="8" ry="3"/><path d="M4 5v7c0 1.66 3.58 3 8 3s8-1.34 8-3V5"/><path d="M4 12v7c0 1.66 3.58 3 8 3s8-1.34 8-3v-7"/>`)
	// Sliders is the settings panel: three rails with a knob each, the way every
	// preferences dialog draws itself.
	Sliders = svg(`<path d="M4 6h9"/><path d="M17 6h3"/><circle cx="15" cy="6" r="2"/><path d="M4 12h4"/><path d="M12 12h8"/><circle cx="10" cy="12" r="2"/><path d="M4 18h9"/><path d="M17 18h3"/><circle cx="15" cy="18" r="2"/>`)
	// GitBranch is the changes drawer: two commits on the left, one on a branch
	// of its own, which is the shape every client draws for the same idea.
	GitBranch = svg(`<circle cx="6" cy="6" r="2.5"/><circle cx="6" cy="18" r="2.5"/><circle cx="18" cy="8" r="2.5"/><path d="M6 8.5v7"/><path d="M18 10.5v1a4 4 0 0 1-4 4H8.5"/>`)
	PlugZap   = svg(`<path d="M6.3 20.3a2.4 2.4 0 0 0 3.4 0L12 18l-6-6-2.3 2.3a2.4 2.4 0 0 0 0 3.4Z"/><path d="m2 22 3-3"/><path d="M7.5 13.5 10 11"/><path d="M10.5 16.5 13 14"/><path d="m18 3-4 4h6l-4 4"/>`)
)

func svg(body string) *ui.SVG {
	return ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">` + body + `</svg>`))
}
