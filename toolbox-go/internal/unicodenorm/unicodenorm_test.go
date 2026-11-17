package unicodenorm

import (
	"testing"
)

func TestKnownForms(t *testing.T) {
	cases := []struct{ input, nfd, nfkc string }{
		{"é", "e\u0301", "é"},
		{"ﬁ", "ﬁ", "fi"},                 // ligature: compatibility only
		{"Ａ１", "Ａ１", "A1"},               // fullwidth
		{"①", "①", "1"},                  // enclosed alphanumeric
		{"㍿", "㍿", "株式会社"},               // squared CJK
		{"｀", "｀", "`"},                  // fullwidth grave, the one with a backquote
		{"`", "\u0060", "\u0060"},        // Greek varia decomposes to a backquote
		{"한", "\u1112\u1161\u11AB", "한"}, // Hangul: arithmetic decomposition
		{"가", "\u1100\u1161", "가"},
		{"\u1100\u1161\u11A8", "\u1100\u1161\u11A8", "각"},   // jamo compose back
		{"q\u0307\u0323", "q\u0323\u0307", "q\u0323\u0307"}, // canonical ordering by class
		{"ẛ", "ſ\u0307", "ṡ"},                               // canonical decomposition is recursive
		{"\u00A0", "\u00A0", " "},                           // no-break space
		{"\U0001E030", "\U0001E030", "\u0430"},              // added after Unicode 13: only the newer tables fold it
		{"plain ASCII", "plain ASCII", "plain ASCII"},
		{"", "", ""},
	}
	for _, testCase := range cases {
		if got := NFD(testCase.input); got != testCase.nfd {
			t.Errorf("NFD(%q) = %q, want %q", testCase.input, got, testCase.nfd)
		}
		if got := NFKC(testCase.input); got != testCase.nfkc {
			t.Errorf("NFKC(%q) = %q, want %q", testCase.input, got, testCase.nfkc)
		}
	}
}

// Across the whole code space, normalization must be idempotent and canonically ordered: that is
// what makes a single expansion step safe and keeps fuzzed matching stable.
func TestNormalizationIsIdempotentAndOrdered(t *testing.T) {
	load()
	for cp := rune(0); cp < 0x110000; cp++ {
		if cp >= 0xD800 && cp <= 0xDFFF {
			continue
		}
		single := string(cp)
		composed := NFKC(single)
		if again := NFKC(composed); again != composed {
			t.Fatalf("NFKC is not idempotent for U+%04X: %q then %q", cp, composed, again)
		}
		decomposed := NFD(single)
		if again := NFD(decomposed); again != decomposed {
			t.Fatalf("NFD is not idempotent for U+%04X", cp)
		}
		if !canonicallyOrdered([]rune(decomposed)) {
			t.Fatalf("NFD of U+%04X is not canonically ordered", cp)
		}
	}
}

// canonicallyOrdered checks that combining classes never decrease inside a non-starter run.
func canonicallyOrdered(runes []rune) bool {
	previous := uint8(0)
	for _, ch := range runes {
		class := data.combining[ch]
		if class == 0 {
			previous = 0
			continue
		}
		if class < previous {
			return false
		}
		previous = class
	}
	return true
}
