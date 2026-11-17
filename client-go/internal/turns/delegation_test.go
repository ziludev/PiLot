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
	if !ok || run.ConversationID != 475 || len(run.Roles) != 1 || run.Roles[0] != "designer" || run.Failed != 0 {
		t.Fatalf("single delegation = %+v ok=%v", run, ok)
	}

	parallel := entry(2, "pi.tool-result", []map[string]any{{
		"role": "toolResult", "toolName": "delegate_parallel",
		"content": []any{map[string]any{"type": "text", "text": "【designer】…"}},
		"details": map[string]any{"roles": []any{"designer", "reviewer"}, "failed": float64(1)},
	}})
	run, ok = DelegationOf(parallel)
	if !ok || run.ConversationID != 0 || len(run.Roles) != 2 || run.Failed != 1 {
		t.Fatalf("parallel delegation = %+v ok=%v", run, ok)
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
