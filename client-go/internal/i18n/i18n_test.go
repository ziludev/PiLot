package i18n

import (
	"strings"
	"testing"
)

// P0 keeps the window's strings in one small table; a key nobody draws costs a
// reader a row and a translator a translation, so the ceiling is enforced here.
// 201 keys: the changes drawer names its comparison base, says what moving it
// moves and what a branch it does not have looks like; 194 keys: the projects drawer names the repositories, what adding one can
// answer (not a repo, missing, already there) and what forgetting one does to
// its chats; 178 keys: the changes drawer can commit, push and merge, and each verb says
// what it would act on and what came back — a conflict, a dirty side, a refusal;
// 154 keys: the changes drawer can read a file whole, so it names the switch,
// an empty file and a cut-short one;
// 150 keys: the task dialog names a task's kind, chat, clock and children, and
// the usage calendar names its four months, its cost and its weekday marks;
// 122 keys: the message bar names its verbs and the jump pill says where it goes;
// 117 keys: the memory drawer named its three scopes, the settings panel took the
// rail footer's switches, the find bar counts its hits, and the changes drawer
// names a patch, a branch and a way back. The point of the ceiling is that it
// only moves with a reason. 196 keys: the base picker went away, and with it the
// five strings that asked which branch to measure from — the chip only names it.
// 205 keys: the banners say how a turn ended and what a write wants, and the
// settings panel grew the switch that silences them, and the changes drawer
// learned how to hand the checkout to Finder or the editor.
const maxKeys = 207

func TestDictionariesMatch(t *testing.T) {
	keys := Keys()
	if len(keys) == 0 {
		t.Fatal("no keys at all")
	}
	if len(keys) > maxKeys {
		t.Fatalf("got %d keys, ceiling is %d", len(keys), maxKeys)
	}
	if len(zh) != len(en) {
		t.Fatalf("zh has %d keys, en has %d", len(zh), len(en))
	}
	for _, k := range keys {
		if _, ok := en[k]; !ok {
			t.Errorf("key %q is only in zh", k)
		}
		if _, ok := zh[k]; !ok {
			t.Errorf("key %q is only in en", k)
		}
		if strings.TrimSpace(zh[k]) == "" {
			t.Errorf("key %q is empty in zh", k)
		}
		if strings.TrimSpace(en[k]) == "" {
			t.Errorf("key %q is empty in en", k)
		}
	}
}

func TestTranslation(t *testing.T) {
	useLang(t, EN)
	if got := T("send"); got != "Send" {
		t.Fatalf("en send = %q", got)
	}
	useLang(t, ZH)
	if got := T("send"); got != "发送" {
		t.Fatalf("zh send = %q", got)
	}
	if got := TF("queueCount", map[string]any{"n": 2}); got != "2 条排队" {
		t.Fatalf("zh queueCount = %q", got)
	}
	if got := T("no.such.key"); got != "no.such.key" {
		t.Fatalf("missing key rendered as %q", got)
	}
}

// useLang switches the package language for one test and restores it after.
func useLang(t *testing.T, l Lang) {
	t.Helper()
	prev := Current()
	Set(l)
	t.Cleanup(func() { Set(prev) })
}

// The window is written in Chinese; English is an opt-in for a reader who asks
// for it, never a side effect of the Mac's locale.
func TestDetectLang(t *testing.T) {
	t.Setenv("PILOT_LANG", "")
	if got := detectLang(); got != ZH {
		t.Fatalf("default = %q, want zh", got)
	}
	t.Setenv("PILOT_LANG", "en")
	if got := detectLang(); got != EN {
		t.Fatalf("PILOT_LANG=en = %q, want en", got)
	}
	t.Setenv("PILOT_LANG", "fr")
	if got := detectLang(); got != ZH {
		t.Fatalf("PILOT_LANG=fr = %q, want zh", got)
	}
}
