package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"pitta/internal/i18n"

	"github.com/egoist/mygo/ui"
)

// TestShortcutSheetListsTheKeys: the sheet is only worth a dialog if it names the
// keys that actually do something. Every line has to be registered in View.
func TestShortcutSheetListsTheKeys(t *testing.T) {
	i18n.Set(i18n.EN)
	a := fixtureApp(t, false)

	want := []string{"⌘K", "⌘P", "⌘J", "⇧⌘E", "⌘⇧/"}
	got := a.shortcuts()
	if len(got) != len(want) {
		t.Fatalf("sheet has %d lines, want %d: %v", len(got), len(want), got)
	}
	for i, s := range got {
		if s.keys != want[i] {
			t.Fatalf("line %d keys = %q, want %q", i, s.keys, want[i])
		}
		if s.label == "" {
			t.Fatalf("line %d has no label", i)
		}
	}
}

// TestShortcutSheetOpensAndCloses: the key toggles, and opening it puts the
// palette away so the two dialogs do not fight over the keyboard.
func TestShortcutSheetOpensAndCloses(t *testing.T) {
	i18n.Set(i18n.EN)
	a := fixtureApp(t, false)
	a.palette.open = true

	a.toggleCheatsheet()
	if !a.cheatsheet {
		t.Fatal("sheet did not open")
	}
	if a.palette.open {
		t.Fatal("palette stayed open under the sheet")
	}
	tt := ui.NewTester(a.View, 1180, 820)
	tt.Frame()
	if !tt.HasText("Keyboard shortcuts") || !tt.HasText("⌘P") {
		t.Fatalf("sheet not drawn: %v", tt.Texts())
	}

	a.toggleCheatsheet()
	if a.cheatsheet {
		t.Fatal("sheet did not close")
	}
}

// TestExportHTMLIsOneReadableFile: a page that opens by double click, with the
// words in prose and the steps folded — and nothing that needs a script.
func TestExportHTMLIsOneReadableFile(t *testing.T) {
	i18n.Set(i18n.EN)
	a := fixtureApp(t, false)
	th := a.threads[a.active]
	rows, _ := projectEntries(a.active, th.entries)

	page := chatHTML("Deploy checklist", rows, a.now)
	for _, want := range []string{"<!doctype html>", "<title>Deploy checklist</title>",
		"<details>", "<summary>", "PiLot", i18n.T("exportYou")} {
		if !strings.Contains(page, want) {
			t.Fatalf("page has no %q", want)
		}
	}
	if strings.Contains(page, "<script") {
		t.Fatal("page carries a script; it needs none")
	}
	// The chat's own words are escaped, not injected.
	a.draft = ""
	if page := chatHTML("x", []threadRow{{kind: rowUser, text: "<b>bold</b> & co"}}, a.now); strings.Contains(page, "<b>bold</b>") {
		t.Fatal("the reader's text was not escaped")
	}
}

// TestExportHTMLLandsNextToTheMarkdown: same name, other extension, and the
// notice says where it went.
func TestExportHTMLLandsNextToTheMarkdown(t *testing.T) {
	i18n.Set(i18n.EN)
	a := fixtureApp(t, false)
	dir := t.TempDir()

	a.exportHTMLTo(dir)
	tt := ui.NewTester(a.View, 1180, 820)
	tt.Frame()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || !strings.HasSuffix(entries[0].Name(), ".html") ||
		!strings.HasPrefix(entries[0].Name(), "PiLot-") {
		t.Fatalf("files = %v, want one PiLot-*.html", entries)
	}
	if a.notice == "" || !strings.Contains(a.notice, ".html") {
		t.Fatalf("notice = %q, want the path it wrote", a.notice)
	}
	if !a.noticeOK {
		t.Fatalf("a finished export reads as a failure: %q", a.notice)
	}
	body, err := os.ReadFile(filepath.Join(dir, entries[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "</html>") {
		t.Fatal("the page is not closed")
	}
}

// TestExportHTMLNamePairsWithTheMarkdown: one chat taken at one moment yields a
// .md and a .html that sit next to each other.
func TestExportHTMLNamePairsWithTheMarkdown(t *testing.T) {
	i18n.Set(i18n.EN)
	a := fixtureApp(t, false)
	md := exportName("Deploy checklist", a.now)
	page := exportHTMLName("Deploy checklist", a.now)
	if strings.TrimSuffix(page, ".html") != strings.TrimSuffix(md, ".md") {
		t.Fatalf("names do not pair: %q vs %q", md, page)
	}
}
