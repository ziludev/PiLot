package app

import (
	"context"
	"os"
	"testing"
	"time"

	"pitta/internal/api"
	"pitta/internal/protocol"
)

// TestLiveApprovalDecision walks the approval path against the running agent:
// a guard-hooked bash call parks the turn until someone answers, this client
// reads it from GET /api/approvals and denies it through POST /api/approvals.
// The command is `sudo -n true`, which the sudo rule parks and which cannot
// touch anything whether it runs or not.
//
// It costs one model call and waits on the guard, so it only runs when asked:
//
//	PITTA_APPROVAL=1 go test ./internal/app -run TestLiveApprovalDecision -v
func TestLiveApprovalDecision(t *testing.T) {
	if os.Getenv("PITTA_APPROVAL") == "" {
		t.Skip("set PITTA_APPROVAL=1 to run against the live agent")
	}
	cfg := liveConfig()
	a := New(cfg)
	a.init()
	if !a.reachable {
		t.Fatalf("bridge not reachable at %s (%s)", cfg.BaseURL, a.downReason)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	project := ""
	if len(a.projects) > 0 {
		project = a.projects[0].ID
	}
	conversationID, surfaceID, err := a.api.NewConversation(ctx, project)
	if err != nil {
		t.Fatalf("new conversation: %v", err)
	}
	t.Logf("conversation=%d surface=%s", conversationID, surfaceID)

	// A bash call the guard parks; the reason it is parked is the sudo rule.
	if err := a.api.Submit(ctx, api.SubmitRequest{
		ConversationID: conversationID,
		Text:           "Use the bash tool to run exactly this command, then show its output: sudo -n true",
	}); err != nil {
		t.Fatalf("submit: %v", err)
	}

	var pending *protocol.PendingApproval
	deadline := time.Now().Add(3 * time.Minute)
	for time.Now().Before(deadline) {
		list, err := a.api.Approvals(ctx)
		if err != nil {
			t.Fatalf("list approvals: %v", err)
		}
		for i := range list {
			// The list carries the surface that asked; other chats may have
			// their own pending calls, so match ours by surface.
			if list[i].SurfaceID == surfaceID {
				pending = &list[i]
				break
			}
		}
		if pending != nil {
			break
		}
		time.Sleep(time.Second)
	}
	if pending == nil {
		surfaces, _ := a.api.Conversation(ctx, conversationID)
		t.Fatalf("no approval appeared within 3m; conversation had %d entries", len(surfaces.Entries))
	}
	t.Logf("pending callId=%s tool=%s reason=%q waited=%ds",
		pending.CallID, pending.Tool, pending.Reason, (time.Now().UnixMilli()-pending.CreatedAt)/1000)

	if err := a.api.DecideApproval(ctx, api.ApprovalDecision{
		CallID:         pending.CallID,
		Approved:       false,
		ConversationID: conversationID,
	}); err != nil {
		t.Fatalf("deny approval: %v", err)
	}
	list, err := a.api.Approvals(ctx)
	if err != nil {
		t.Fatalf("list approvals after the decision: %v", err)
	}
	for _, entry := range list {
		if entry.CallID == pending.CallID {
			t.Fatal("approval still pending after the decision")
		}
	}
	t.Logf("denied callId=%s via the client; %d approvals left", pending.CallID, len(list))
}
