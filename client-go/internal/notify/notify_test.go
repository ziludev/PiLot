package notify

import (
	"strings"
	"testing"
	"time"
)

// seen swaps Sender for a channel and puts it back after the test.
func seen(t *testing.T) <-chan [3]string {
	t.Helper()
	saved := Sender
	t.Cleanup(func() { Sender = saved })
	posts := make(chan [3]string, 4)
	Sender = func(id, title, body string) error {
		posts <- [3]string{id, title, body}
		return nil
	}
	return posts
}

// TestAnswerLeadsWithTheChatName is the fix for a notification that read the
// same whichever chat finished: the name goes in front of what happened, and the
// answer's opening words go underneath.
func TestAnswerLeadsWithTheChatName(t *testing.T) {
	posts := seen(t)
	Answer("蓝鲸与那个代号的排查", "  第一行的回答\n 还有一行 ", 12)

	select {
	case got := <-posts:
		if want := "蓝鲸与那个代号的排查 · " + Title; got[1] != want {
			t.Fatalf("title = %q, want %q", got[1], want)
		}
		if got[2] != "第一行的回答 还有一行" {
			t.Fatalf("body = %q", got[2])
		}
		if got[0] != IDFor(12) {
			t.Fatalf("id = %q, want %q", got[0], IDFor(12))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("nothing was sent")
	}
}

// TestAnswerWithoutAName keeps the app's own name in front of a chat that has no
// name to lead with.
func TestAnswerWithoutAName(t *testing.T) {
	posts := seen(t)
	Answer("  ", "hello", 3)
	select {
	case got := <-posts:
		if want := Agent + " · " + Title; got[1] != want {
			t.Fatalf("title = %q, want %q", got[1], want)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("nothing was sent")
	}
}

// TestAnswerClipsLongNames checks a long chat name is cut with a mark rather
// than clipped by the system mid-glyph.
func TestAnswerClipsLongNames(t *testing.T) {
	posts := seen(t)
	long := strings.Repeat("很长的会话标题", 6)
	Answer(long, "hello", 4)
	select {
	case got := <-posts:
		name, rest, ok := strings.Cut(got[1], " · ")
		if !ok {
			t.Fatalf("title = %q, want name and what happened", got[1])
		}
		if len([]rune(name)) != nameLimit+1 || !strings.HasSuffix(name, "…") {
			t.Fatalf("name = %q (%d runes), want %d runes and a mark", name, len([]rune(name)), nameLimit+1)
		}
		if rest != Title {
			t.Fatalf("title = %q, want it to end with %q", got[1], Title)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("nothing was sent")
	}
}

// TestAnswerIgnoresAnEmptyAnswer keeps a frame that carries no words from
// buzzing the owner with a blank notification.
func TestAnswerIgnoresAnEmptyAnswer(t *testing.T) {
	posts := seen(t)
	Answer("chat", "   \n\t ", 5)
	select {
	case got := <-posts:
		t.Fatalf("sent %v for an empty answer", got)
	case <-time.After(80 * time.Millisecond):
	}
}
