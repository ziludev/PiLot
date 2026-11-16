package app

import (
	"strings"
	"testing"

	"pitta/internal/protocol"
)

// TestExportWritesTheChatAsMarkdown checks the file a reader keeps: title,
// question, the tool step, the answer and the cost line.
func TestExportWritesTheChatAsMarkdown(t *testing.T) {
	a := New(Config{})
	a.entries = turnEntries(11, "why is the sky blue", "Rayleigh scattering.")
	a.surfaces = []protocol.Surface{{ConversationID: 11, Title: "why is the sky blue", Cost: 0.42, Tokens: 1234}}
	a.current = 11
	name, text := a.exportMarkdown()
	if !strings.HasPrefix(name, "why is the sky blue #11") || !strings.HasSuffix(name, ".md") {
		t.Fatalf("name = %q", name)
	}
	for _, want := range []string{"# why is the sky blue", "## You", "Rayleigh scattering.", "cost $0.42 · 1234 tokens"} {
		if !strings.Contains(text, want) {
			t.Fatalf("export misses %q:\n%s", want, text)
		}
	}
}

// TestExportNamesTheFileSafely checks the characters a file name cannot hold.
func TestExportNamesTheFileSafely(t *testing.T) {
	if got := exportName("a/b: c", 3); strings.ContainsAny(got, "/:") {
		t.Fatalf("name %q keeps a path separator", got)
	}
	if got := exportName("   ", 4); got != "chat #4.md" {
		t.Fatalf("empty title gives %q", got)
	}
}
