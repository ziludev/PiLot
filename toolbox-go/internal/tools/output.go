package tools

import (
	"os"
	"strings"
	"sync"
	"unicode/utf8"
)

// utf8Stream turns the raw bytes read from a child into text.
//
// It holds back a trailing partial rune so a character split across two reads is emitted whole, it
// drops one leading byte-order mark, and it replaces every invalid sequence with a single U+FFFD.
// All three are what the reference's StreamDecoder over TextDecoder does (dist/env/decode.js), and
// the model sees the result, so they are reproduced byte for byte.
type utf8Stream struct {
	pending []byte
	started bool
}

func (s *utf8Stream) write(chunk []byte) string {
	buf := append(s.pending, chunk...)
	s.pending = nil
	hold := incompleteTail(buf)
	text := s.first(decodeUTF8(buf[:len(buf)-hold]))
	if hold > 0 {
		s.pending = append([]byte(nil), buf[len(buf)-hold:]...)
	}
	return text
}

// flush decodes what is left once the stream ended; a truncated rune becomes U+FFFD.
func (s *utf8Stream) flush() string {
	text := s.first(decodeUTF8(s.pending))
	s.pending = nil
	return text
}

// first drops the byte-order mark a stream may start with, once, on the first non-empty text —
// the same place StreamDecoder does it, so a mark split across reads is still dropped.
func (s *utf8Stream) first(text string) string {
	if s.started || text == "" {
		return text
	}
	s.started = true
	return strings.TrimPrefix(text, "\ufeff")
}

// incompleteTail returns how many trailing bytes form an unfinished rune (0-3).
func incompleteTail(buf []byte) int {
	for i := len(buf) - 1; i >= 0 && i >= len(buf)-utf8.UTFMax; i-- {
		if !utf8.RuneStart(buf[i]) {
			continue
		}
		if !utf8.FullRune(buf[i:]) {
			return len(buf) - i
		}
		return 0
	}
	return 0
}

// decodeUTF8 mirrors TextDecoder: a malformed sequence yields one U+FFFD for its maximal subpart,
// not one per byte. "EF BB A" is therefore a replacement and an A, exactly as the reference.
func decodeUTF8(buf []byte) string {
	var out strings.Builder
	out.Grow(len(buf))
	for len(buf) > 0 {
		r, size := decodeRune(buf)
		if size == 0 {
			break
		}
		out.WriteRune(r)
		buf = buf[size:]
	}
	return out.String()
}

// decodeRune consumes one code point or the longest invalid subpart of it, like the WHATWG decoder
// TextDecoder implements: the lead byte plus every continuation that was still in range.
func decodeRune(buf []byte) (rune, int) {
	b := buf[0]
	if b < 0x80 {
		return rune(b), 1
	}
	length, low, high := 0, byte(0x80), byte(0xBF)
	switch {
	case b >= 0xC2 && b <= 0xDF:
		length = 2
	case b >= 0xE0 && b <= 0xEF:
		length = 3
		switch b {
		case 0xE0:
			low = 0xA0
		case 0xED:
			high = 0x9F
		}
	case b >= 0xF0 && b <= 0xF4:
		length = 4
		switch b {
		case 0xF0:
			low = 0x90
		case 0xF4:
			high = 0x8F
		}
	default:
		return utf8.RuneError, 1
	}
	consumed := 1
	for i := 1; i < length; i++ {
		if i >= len(buf) {
			return utf8.RuneError, consumed
		}
		min, max := byte(0x80), byte(0xBF)
		if i == 1 {
			min, max = low, high
		}
		if buf[i] < min || buf[i] > max {
			return utf8.RuneError, consumed
		}
		consumed++
	}
	value := rune(b) & (0x7F >> uint(length))
	for i := 1; i < length; i++ {
		value = value<<6 | rune(buf[i]&0x3F)
	}
	return value, length
}

// Thresholds copied from pi-durable: bash spills its output to a file once it
// exceeds 50KB or 2000 lines, so the model can still read the rest.
const (
	spillAfterBytes = 50 * 1024
	spillAfterLines = 2000
)

// spill saves the complete output to a temp file once it outgrows what the agent
// keeps, and reports the path back with the result.
//
// It is written from the first byte, not from the moment the threshold is crossed,
// so the spilled file really is complete.
type spill struct {
	mu      sync.Mutex
	path    string
	file    *os.File
	prefix  [][]byte
	bytes   int
	newline int
}

// add records one chunk and returns nothing; the caller streams the same chunk.
func (s *spill) add(chunk []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(chunk) == 0 {
		return
	}
	if s.file == nil {
		s.bytes += len(chunk)
		for _, b := range chunk {
			if b == '\n' {
				s.newline++
			}
		}
		lines := s.newline
		if chunk[len(chunk)-1] != '\n' {
			lines++
		}
		if s.bytes <= spillAfterBytes && lines <= spillAfterLines {
			s.prefix = append(s.prefix, append([]byte(nil), chunk...))
			return
		}
		if err := s.open(); err != nil {
			return
		}
	}
	_, _ = s.file.Write(chunk)
}

// open creates the temp file and writes the buffered prefix into it. The shape of
// the path matches the agent's own spill files (`<tmp>/tmp-XXXX/pi-output-*.log`),
// so the location a user sees does not depend on which path ran the command.
func (s *spill) open() error {
	dir, err := os.MkdirTemp("", "tmp-")
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(dir, "pi-output-*.log")
	if err != nil {
		return err
	}
	s.file = file
	s.path = file.Name()
	for _, chunk := range s.prefix {
		if _, err := file.Write(chunk); err != nil {
			return err
		}
	}
	s.prefix = nil
	return nil
}

// Path is the spill file, or "" when the output fit.
func (s *spill) Path() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.path
}

// close finishes the file. The path stays valid: the agent hands it to the model.
func (s *spill) close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.file == nil {
		return
	}
	_ = s.file.Close()
	s.file = nil
}
