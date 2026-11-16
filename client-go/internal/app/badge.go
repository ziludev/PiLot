package app

// badgeHook receives the pending approval count. main.go sets it to the Dock's
// badge so the headless tests, which have no application, stay quiet.
var badgeHook func(int)

// SetBadgeHook lets main.go push the count to the Dock icon.
func SetBadgeHook(fn func(int)) {
	badgeHook = fn
}
