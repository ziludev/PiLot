package tools

import (
	"os"
	"strings"
	"sync"
	"unicode/utf8"
)

// utf8Stream turns the raw bytes read from a child into text.
//
// It holds back a trailing partial rune, so a character split across two reads is
// emitted whole, and it replaces every invalid byte with one U+FFFD, which is what
// TextDecoder does on the Node side. Both behaviours are visible to the model, so
// they are reproduced rather than approximated.
type utf8Stream struct {
	pending []byte
}

func (s *utf8Stream) write(chunk []byte) string {
	buf := append(s.pending, chunk...)
	s.pending = nil
	hold := incompleteTail(buf)
	text := decodeUTF8(buf[:len(buf)-hold])
	if hold > 0 {
		s.pending = append([]byte(nil), buf[len(buf)-hold:]...)
	}
	return text
}

// flush decodes what is left once the stream ended; a truncated rune becomes U+FFFD.
func (s *utf8Stream) flush() string {
	text := decodeUTF8(s.pending)
	s.pending = nil
	return text
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

func decodeUTF8(buf []byte) string {
	var out strings.Builder
	out.Grow(len(buf))
	for len(buf) > 0 {
		r, size := utf8.DecodeRune(buf)
		if r == utf8.RuneError && size == 1 {
			out.WriteRune(utf8.RuneError)
			buf = buf[1:]
			continue
		}
		out.Write(buf[:size])
		buf = buf[size:]
	}
	return out.String()
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
