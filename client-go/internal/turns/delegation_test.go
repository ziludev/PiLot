package turns

import "testing"

// TestDelegationOfReadsSingleAndParallelChildren pins the two shapes the
// delegate tools really write: one child with its conversation, and a parallel
// batch that only reports roles and how many failed.
func TestDelegationOfReadsSingleAndParallelChildren(t *testing.T) {
	single := entry(1, "pi.tool-result", []map[string]any{{
		"role": "toolResult", "toolName": "delegate",
		"content": []any{map[string]any{"type": "text", "text": "【designer 的产出】\n…"}},
		"details": map[string]any{"conversationId": 475, "role": "designer"},
	}})
	run, ok := DelegationOf(single)
	if !ok || len(run.Children) != 1 {
		t.Fatalf("single delegation = %+v ok=%v", run, ok)
	}
	if child := run.Children[0]; child.Role != "designer" || child.ConversationID != 475 || child.Failed {
		t.Fatalf("single child = %+v", child)
	}

	parallel := entry(2, "pi.tool-result", []map[string]any{{
		"role": "toolResult", "toolName": "delegate_parallel",
		"content": []any{map[string]any{"type": "text", "text": "【designer】…"}},
		"details": map[string]any{"roles": []any{"designer", "reviewer"}, "failed": float64(1)},
	}})
	run, ok = DelegationOf(parallel)
	if !ok || len(run.Children) != 2 || run.Failed != 1 {
		t.Fatalf("parallel delegation = %+v ok=%v", run, ok)
	}
	if child := run.Children[0]; child.Role != "designer" || child.ConversationID != 0 {
		t.Fatalf("first parallel child = %+v", child)
	}
}

// TestDelegationOfIgnoresOtherCalls keeps a plain bash result from being read as
// a child: the details a tool writes are only a delegation when the tool is one.
func TestDelegationOfIgnoresOtherCalls(t *testing.T) {
	bash := entry(1, "pi.tool-result", []map[string]any{{
		"role": "toolResult", "toolName": "bash",
		"content": []any{map[string]any{"type": "text", "text": "ok"}},
		"details": map[string]any{"conversationId": 7, "role": "designer"},
	}})
	if run, ok := DelegationOf(bash); ok {
		t.Fatalf("bash read as delegation: %+v", run)
	}
	// A delegate that has not answered yet carries no details at all.
	running := entry(2, "pi.tool-result", []map[string]any{{
		"role": "toolResult", "toolName": "delegate",
		"content": []any{map[string]any{"type": "text", "text": ""}},
	}})
	if run, ok := DelegationOf(running); ok {
		t.Fatalf("answerless delegate read as delegation: %+v", run)
	}
}

// TestDelegationOfReadsEachParallelChild is the shape delegate_parallel writes
// now: every child with its own conversation, so each can be opened on its own.
func TestDelegationOfReadsEachParallelChild(t *testing.T) {
	batch := entry(1, "pi.tool-result", []map[string]any{{
		"role": "toolResult", "toolName": "delegate_parallel",
		"content": []any{map[string]any{"type": "text", "text": "【implementer】…"}},
		"details": map[string]any{
			"roles": []any{"implementer", "reviewer"}, "failed": float64(1),
			"children": []any{
				map[string]any{"role": "implementer", "conversationId": float64(477)},
				map[string]any{"role": "reviewer", "conversationId": float64(478), "failed": true},
			},
		},
	}})
	run, ok := DelegationOf(batch)
	if !ok || len(run.Children) != 2 {
		t.Fatalf("batch = %+v ok=%v", run, ok)
	}
	first, second := run.Children[0], run.Children[1]
	if first.ConversationID != 477 || first.Failed {
		t.Errorf("first child = %+v", first)
	}
	if second.Role != "reviewer" || second.ConversationID != 478 || !second.Failed {
		t.Errorf("second child = %+v", second)
	}
}

// TestDelegationOfKeepsAFailedChildsChat: a delegate that failed still names the
// conversation it failed in, which is the one worth reading.
func TestDelegationOfKeepsAFailedChildsChat(t *testing.T) {
	failed := entry(1, "pi.tool-result", []map[string]any{{
		"role": "toolResult", "toolName": "delegate", "isError": true,
		"content": []any{map[string]any{"type": "text", "text": "失败：designer: timeout"}},
		"details": map[string]any{"role": "designer", "failed": true, "conversationId": float64(475)},
	}})
	run, ok := DelegationOf(failed)
	if !ok || len(run.Children) != 1 {
		t.Fatalf("failed delegation = %+v ok=%v", run, ok)
	}
	if child := run.Children[0]; !child.Failed || child.ConversationID != 475 {
		t.Fatalf("failed child = %+v", child)
	}
}
