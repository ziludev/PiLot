package main

import (
	"os"
	"path/filepath"
	"testing"
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
