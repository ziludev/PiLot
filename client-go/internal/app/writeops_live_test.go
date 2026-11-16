package app

import (
	"context"
	"os"
	"testing"

	"pitta/internal/api"
	"pitta/internal/bridge"
	"pitta/internal/protocol"
)

// TestLiveWritePaths exercises the bridge's write endpoints that are safe to
// run against the everyday agent: a model switch (restored afterwards), an
// idempotent rename, and a push dry run, which asks the remote but changes
// nothing.
//
// Commit, merge and approval decisions are deliberately absent: they change the
// owner's worktree or unblock a running tool call, so they are covered by
// fixtures in internal/side and internal/app instead (see docs/parity.md).
//
// Run with PITTA_LIVE=1.
func TestLiveWritePaths(t *testing.T) {
	if os.Getenv("PITTA_LIVE") != "1" {
		t.Skip("set PITTA_LIVE=1 to touch the running agent")
	}
	ctx := context.Background()
	client := api.New(bridge.BaseURL(), bridge.Token())

	state, err := client.State(ctx)
	if err != nil {
		t.Fatalf("state: %v", err)
	}
	// The newest chat that still has a surface: an old conversation whose
	// surface is gone cannot be renamed.
	conversationID := 0
	surfaceID := ""
	for _, surface := range state.Surfaces {
		if surface.ConversationID != 0 && surface.SurfaceID != "" {
			conversationID = surface.ConversationID
			surfaceID = surface.SurfaceID
			break
		}
	}
	if conversationID == 0 {
		t.Fatalf("the bridge lists no conversations")
	}
	before, err := client.Conversation(ctx, conversationID)
	if err != nil {
		t.Fatalf("conversation: %v", err)
	}
	if before.Model == nil {
		t.Fatalf("conversation %d reports no model", conversationID)
	}
	original := *before.Model

	// 1. A model switch must be visible in the conversation afterwards, which
	// is also what makes the next turn use it.
	models, err := client.Models(ctx)
	if err != nil {
		t.Fatalf("models: %v", err)
	}
	var target *protocol.ModelEntry
	for i := range models {
		if models[i].Provider != original.Provider || models[i].ID != original.ModelID {
			target = &models[i]
			break
		}
	}
	if target == nil {
		t.Fatalf("no second model to switch to")
	}
	level := ""
	if len(target.ThinkingLevels) > 0 {
		level = target.ThinkingLevels[0]
	}
	if err := client.SetModel(ctx, conversationID, api.ModelChoice{
		Provider: target.Provider, ModelID: target.ID, ThinkingLevel: level,
	}); err != nil {
		t.Fatalf("set model: %v", err)
	}
	after, err := client.Conversation(ctx, conversationID)
	if err != nil {
		t.Fatalf("conversation after switch: %v", err)
	}
	if after.Model == nil || after.Model.Provider != target.Provider || after.Model.ModelID != target.ID {
		t.Fatalf("model did not change: %+v", after.Model)
	}
	t.Logf("model switch ok: %s/%s → %s/%s (thinking %q)",
		original.Provider, original.ModelID, after.Model.Provider, after.Model.ModelID, after.ThinkingLevel)
	if err := client.SetModel(ctx, conversationID, api.ModelChoice{
		Provider: original.Provider, ModelID: original.ModelID, ThinkingLevel: before.ThinkingLevel,
	}); err != nil {
		t.Fatalf("restore model: %v", err)
	}

	// 2. A rename that writes back the title it already has is idempotent, so
	// it proves the endpoint without changing the rail.
	title := ""
	for _, surface := range state.Surfaces {
		if surface.ConversationID == conversationID {
			title = surface.Title
		}
	}
	if title != "" {
		if err := client.SetSurfaceMeta(ctx, api.SurfaceMeta{
			ConversationID: conversationID, SurfaceID: surfaceID, Title: &title,
		}); err != nil {
			t.Fatalf("rename: %v", err)
		}
		t.Logf("rename ok: kept title %q", title)
	} else {
		t.Logf("rename skipped: conversation %d has no title", conversationID)
	}

	// 3. A dry-run push reports what the remote would receive and nothing else.
	push, err := client.Push(ctx, conversationID, true)
	if err != nil {
		t.Logf("dry-run push answered an error (recorded, not fatal): %v", err)
	} else {
		t.Logf("dry-run push ok: status=%q branch=%q message=%q", push.Status, push.Branch, push.Message)
	}
}
