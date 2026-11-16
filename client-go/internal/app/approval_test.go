package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"pitta/internal/api"
	"pitta/internal/protocol"
)

// approvalServer records the decisions the bar makes and answers the reload
// that follows one.
func approvalServer(t *testing.T) (*httptest.Server, <-chan api.ApprovalDecision) {
	t.Helper()
	decisions := make(chan api.ApprovalDecision, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/approvals":
			if r.Method == http.MethodPost {
				var decision api.ApprovalDecision
				_ = json.NewDecoder(r.Body).Decode(&decision)
				decisions <- decision
				_, _ = w.Write([]byte(`{"status":"ok"}`))
				return
			}
			_, _ = w.Write([]byte(`{"approvals":[]}`))
		case "/api/conversation":
			_, _ = w.Write([]byte(`{"entries":[]}`))
		default:
			_, _ = w.Write([]byte(`{}`))
		}
	}))
	t.Cleanup(srv.Close)
	return srv, decisions
}

// TestApprovalBarDecidesTheCall is the approval contract: the blocked call is
// named with its reason and summary, and each button answers the bridge with
// the right decision.
func TestApprovalBarDecidesTheCall(t *testing.T) {
	srv, decisions := approvalServer(t)
	a := New(Config{BaseURL: srv.URL, Token: "t"})
	a.current = 1
	a.approvals = []protocol.PendingApproval{{
		CallID:    "call_9",
		GrantKey:  "bash:git push",
		Tool:      "bash",
		Summary:   "git push origin main",
		Reason:    "pushes to a remote",
		CreatedAt: time.Now().Add(-3 * time.Second).UnixMilli(),
	}}

	tt := ui.NewTester(a.composer, 800, 200)
	tt.Frame()
	for _, want := range []string{"bash", "pushes to a remote", "git push origin main", "Allow", "Allow all in this chat", "Deny"} {
		if !tt.HasText(want) {
			t.Fatalf("%q not drawn; texts=%q", want, tt.Texts())
		}
	}

	if err := tt.Click("Allow all in this chat"); err != nil {
		t.Fatalf("click allow for session: %v", err)
	}
	select {
	case got := <-decisions:
		if got.CallID != "call_9" || !got.Approved || !got.Session || got.ConversationID != 1 {
			t.Fatalf("decision = %+v", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no decision reached the bridge")
	}
}
