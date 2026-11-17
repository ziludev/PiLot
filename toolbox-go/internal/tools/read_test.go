package tools

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

// pngHeader builds a PNG header with the given chunks. Each chunk is a type plus payload.
func pngHeader(chunks ...[2]string) []byte {
	data := []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}
	for _, chunk := range chunks {
		var length [4]byte
		binary.BigEndian.PutUint32(length[:], uint32(len(chunk[1])))
		data = append(data, length[:]...)
		data = append(data, chunk[0]...)
		data = append(data, chunk[1]...)
		data = append(data, 0, 0, 0, 0) // CRC, unchecked by the sniffer
	}
	return data
}

func TestDetectImageMimeType(t *testing.T) {
	ihdr := string([]byte{0, 0, 0, 2, 0, 0, 0, 3, 8, 6, 0, 0, 0})
	still := pngHeader([2]string{"IHDR", ihdr})
	animated := pngHeader([2]string{"IHDR", ihdr}, [2]string{"acTL", string([]byte{0, 0, 0, 2, 0, 0, 0, 0})})

	bmp := make([]byte, 128)
	copy(bmp, "BM")
	binary.LittleEndian.PutUint32(bmp[2:6], 128)
	binary.LittleEndian.PutUint32(bmp[10:14], 54)
	binary.LittleEndian.PutUint32(bmp[14:18], 12)
	binary.LittleEndian.PutUint16(bmp[22:24], 1)
	binary.LittleEndian.PutUint16(bmp[24:26], 24)

	jpeg := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10}
	f7 := []byte{0xFF, 0xD8, 0xFF, 0xF7, 0x00, 0x10}

	cases := []struct {
		name string
		data []byte
		want string
	}{
		{"still png", still, "image/png"},
		{"animated png reads as text", animated, ""},
		{"png without IHDR reads as text", []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A, 0, 0, 0, 9, 'I', 'H', 'D', 'R'}, ""},
		{"jpeg", jpeg, "image/jpeg"},
		{"jpeg with a 4th byte of f7 reads as text", f7, ""},
		{"gif", []byte("GIF89a\x01\x00\x01\x00"), "image/gif"},
		{"bmp is detected but not inline", bmp, "image/bmp"},
		{"bm without a bitmap header reads as text", []byte("BM not really a bitmap"), ""},
		{"plain text", []byte("hello\n"), ""},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := detectImageMimeType(testCase.data); got != testCase.want {
				t.Fatalf("detectImageMimeType = %q, want %q", got, testCase.want)
			}
		})
	}
}

func TestReadBmpIsAnUnsupportedConversion(t *testing.T) {
	dir := t.TempDir()
	bmp := make([]byte, 128)
	copy(bmp, "BM")
	binary.LittleEndian.PutUint32(bmp[2:6], 128)
	binary.LittleEndian.PutUint32(bmp[10:14], 54)
	binary.LittleEndian.PutUint32(bmp[14:18], 12)
	binary.LittleEndian.PutUint16(bmp[22:24], 1)
	binary.LittleEndian.PutUint16(bmp[24:26], 24)
	path := filepath.Join(dir, "tiny.bmp")
	if err := os.WriteFile(path, bmp, 0o644); err != nil {
		t.Fatalf("write bmp: %v", err)
	}
	srv := newTestServer(t)
	out, err := callTool(t, srv, "read", ReadArgs{Path: path}, dir)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if out["isError"] != true {
		t.Fatalf("isError = %v", out["isError"])
	}
	diagnostics, _ := out["diagnostics"].([]any)
	first, _ := diagnostics[0].(map[string]any)
	want := path + " is an image (image/bmp) that needs converting, and no image processor is configured"
	if first["message"] != want {
		t.Fatalf("message = %v, want %q", first["message"], want)
	}
}
