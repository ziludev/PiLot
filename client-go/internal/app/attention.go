package app

import "pitta/internal/protocol"

// Attention says the app wants the user: an approval is waiting. main.go plays
// the system alert and bounces the Dock icon; the call with false means the
// wait is over and the bounce should stop.
var attention func(on bool)

// SetAttention lets main.go sound the alert and bounce the Dock icon.
func SetAttention(fn func(on bool)) { attention = fn }

// notifyAttention compares the pending approvals with the ones already seen
// and only speaks up for a new wait, so a five-second poll does not sound the
// alert over and over.
func (a *App) notifyAttention(approvals []protocol.PendingApproval) {
	if a.secondary {
		return
	}
	waiting := false
	for _, approval := range approvals {
		if !a.seenApprovals[approval.CallID] {
			a.seenApprovals[approval.CallID] = true
			waiting = true
		}
	}
	if len(approvals) == 0 {
		a.seenApprovals = map[string]bool{}
		if a.waitingAlert && attention != nil {
			a.waitingAlert = false
			attention(false)
		}
		return
	}
	if waiting && attention != nil {
		a.waitingAlert = true
		attention(true)
	}
}
