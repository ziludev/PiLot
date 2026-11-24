package main

import (
	"errors"
	"testing"

	lark "github.com/larksuite/oapi-sdk-go/v3"
)

// The sweep below is what the SDK does not do for itself: a tenant token Feishu has invalidated is
// retried once against a rebuilt client, and nothing else is ever retried.

func TestIsStaleTokenMatchesOnlyTokenRejections(t *testing.T) {
	for _, code := range []int{99991663, 99991668} {
		if !isStaleToken(&feishuError{code: code, msg: "Invalid access token"}) {
			t.Fatalf("code %d must count as a stale token", code)
		}
	}
	for _, err := range []error{
		nil,
		&feishuError{code: 230011, msg: "The message was withdrawn."},
		&feishuError{code: 99991400, msg: "rate limited"},
		errors.New("Post \"https://open.feishu.cn\": EOF"),
	} {
		if isStaleToken(err) {
			t.Fatalf("%v must not count as a stale token", err)
		}
	}
	// Wrapped the way server.fail sees it.
	if !isStaleToken(&wrappedError{inner: &feishuError{code: 99991663}}) {
		t.Fatal("an error wrapping feishuError must still be recognised")
	}
}

type wrappedError struct{ inner error }

func (e *wrappedError) Error() string { return "op failed: " + e.inner.Error() }
func (e *wrappedError) Unwrap() error { return e.inner }

func TestGuardRetriesOnceWithARebuiltClient(t *testing.T) {
	builds := 0
	s := &sender{
		clients: newClientSet(func() *lark.Client { builds++; return lark.NewClient("cli_x", "secret") }),
		out:     quiet(t),
	}
	if builds != 1 {
		t.Fatalf("building the sender built %d clients, want 1", builds)
	}

	var seen []*lark.Client
	calls := 0
	err := s.guard("create", func(client *lark.Client) error {
		calls++
		seen = append(seen, client)
		if calls == 1 {
			return &feishuError{code: 99991663, msg: "Invalid access token"}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("guard returned %v, want the retry to succeed", err)
	}
	if calls != 2 || builds != 2 {
		t.Fatalf("calls=%d builds=%d, want 2 and 2", calls, builds)
	}
	if seen[0] == seen[1] {
		t.Fatal("the retry reused the client that was rejected; the token cache would still be stale")
	}
}

func TestGuardDoesNotRetryAnythingElse(t *testing.T) {
	builds := 0
	s := &sender{
		clients: newClientSet(func() *lark.Client { builds++; return lark.NewClient("cli_x", "secret") }),
		out:     quiet(t),
	}
	calls := 0
	timeout := errors.New("context deadline exceeded")
	err := s.guard("reply", func(*lark.Client) error {
		calls++
		return timeout
	})
	if !errors.Is(err, timeout) || calls != 1 || builds != 1 {
		t.Fatalf("err=%v calls=%d builds=%d: a timeout must be reported once, never retried", err, calls, builds)
	}
}

func TestGuardStopsAfterOneRetry(t *testing.T) {
	s := &sender{
		clients: newClientSet(func() *lark.Client { return lark.NewClient("cli_x", "secret") }),
		out:     quiet(t),
	}
	calls := 0
	err := s.guard("patch", func(*lark.Client) error {
		calls++
		return &feishuError{code: 99991668, msg: "tenant token invalid"}
	})
	if calls != 2 {
		t.Fatalf("calls=%d, want 2: one attempt and one retry, never a loop", calls)
	}
	if !isStaleToken(err) {
		t.Fatalf("err=%v, want the stale-token error surfaced", err)
	}
}

func TestResetRebuildsOncePerRejectedClient(t *testing.T) {
	builds := 0
	set := newClientSet(func() *lark.Client { builds++; return lark.NewClient("cli_x", "secret") })
	stale := set.get()
	// Two calls that both held the same stale client: the second reset must be a no-op.
	set.reset(stale)
	set.reset(stale)
	if builds != 2 {
		t.Fatalf("builds=%d, want 2: one live client and one replacement", builds)
	}
	if set.get() == stale {
		t.Fatal("reset kept the rejected client")
	}
}
