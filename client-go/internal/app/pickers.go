package app

import (
	"log"

	"github.com/egoist/mygo"

	"pitta/internal/i18n"
)

// picker opens the system's file panel. main.go points it at mygo.Dialog; the
// headless tests leave it nil and fall back to the path field, which is also
// what a platform without a native panel gets.
var picker func(opts mygo.OpenDialogOptions) ([]string, error)

// SetPicker lets main.go hand the app the native open panel.
func SetPicker(fn func(opts mygo.OpenDialogOptions) ([]string, error)) {
	picker = fn
}

// chooseImages attaches the pictures the user picks in the system panel, the
// flow Photos-style clients have and a web page cannot open by itself.
func (a *App) chooseImages() {
	if picker == nil {
		a.apply(func() { a.imageOpen = true; a.invalidate() })
		return
	}
	go func() {
		paths, err := picker(mygo.OpenDialogOptions{
			Title:    i18n.T("attachImage"),
			Filters:  []mygo.FileFilter{{Name: "Images", MIMETypes: []string{"image/*"}}},
			Multiple: true,
		})
		if err != nil {
			log.Printf("app: open panel: %v", err)
			a.apply(func() { a.imageOpen = true; a.invalidate() })
			return
		}
		for _, path := range paths {
			a.attachImage(path)
		}
	}()
}

// chooseProject asks for a directory and switches the rail to it.
func (a *App) chooseProject() {
	if picker == nil {
		a.apply(func() {
			a.projectOpen = true
			a.invalidate()
		})
		return
	}
	go func() {
		paths, err := picker(mygo.OpenDialogOptions{
			Title:     i18n.T("openProject"),
			Directory: true,
		})
		if err != nil || len(paths) == 0 {
			if err != nil {
				log.Printf("app: open panel: %v", err)
			}
			return
		}
		a.apply(func() {
			a.projectPath = paths[0]
			a.submitProject()
		})
	}()
}
