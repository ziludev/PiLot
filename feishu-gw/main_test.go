package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher"
)

func TestParseEnvLine(t *testing.T) {
	cases := []struct {
		line      string
		key, want string
		ok        bool
	}{
		{"FEISHU_APP_ID=cli_abc", "FEISHU_APP_ID", "cli_abc", true},
		{`  FEISHU_APP_SECRET="s3cr3t"  `, "FEISHU_APP_SECRET", "s3cr3t", true},
		{"FEISHU_APP_SECRET='a=b'", "FEISHU_APP_SECRET", "a=b", true},
		{"# a comment", "", "", false},
		{"", "", "", false},
		{"NO_EQUALS", "", "", false},
		{"=value", "", "", false},
	}
	for _, c := range cases {
		key, value, ok := parseEnvLine(c.line)
		if ok != c.ok || key != c.key || value != c.want {
			t.Errorf("%q → (%q, %q, %v), want (%q, %q, %v)", c.line, key, value, ok, c.key, c.want, c.ok)
		}
	}
}

func TestLoadEnvKeepsWhatTheLauncherSet(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	body := "FEISHU_APP_ID=from_file\nFEISHU_APP_SECRET=secret_from_file\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FEISHU_APP_ID", "from_env")
	// t.Setenv registers the key, so the file must not overwrite it; the second key is read.
	if err := loadEnv(path); err != nil {
		t.Fatal(err)
	}
	if got := os.Getenv("FEISHU_APP_ID"); got != "from_env" {
		t.Errorf("environment was overwritten: %q", got)
	}
	if got := os.Getenv("FEISHU_APP_SECRET"); got != "secret_from_file" {
		t.Errorf("file value was not loaded: %q", got)
	}
}

// A reaction used to come back from the dispatcher as "not found handler", which the SDK logged as
// [Error] on every tap — 80 lines in thirteen hours, most of that log. Asking for the handler to
// exist is the whole fix: nothing forward, nothing fails, the tap is just counted.
func TestAReactionIsCountedAndNotAnError(t *testing.T) {
	state := &stats{}
	events := dispatcher.NewEventDispatcher("", "")
	wireReactions(events, state)

	payload := []byte(`{
		"schema": "2.0",
		"header": {"event_id": "e1", "event_type": "im.message.reaction.created_v1", "create_time": "1700000000", "token": "", "app_id": "cli_x", "tenant_key": "t"},
		"event": {"message_id": "om_1", "operator_type": "user", "user_id": {"open_id": "ou_1"}, "reaction_type": {"emoji_type": "THUMBSUP"}}
	}`)
	if _, err := events.Do(context.Background(), payload); err != nil {
		t.Fatalf("a tapped reaction errored: %v", err)
	}
	if got := state.reactions.Load(); got != 1 {
		t.Fatalf("counted %d reactions, want 1", got)
	}
}
