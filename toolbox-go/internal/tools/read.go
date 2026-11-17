package tools

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"strconv"
	"strings"
	"time"

	"pitta/toolbox/internal/proto"
	"pitta/toolbox/internal/server"
)

const (
	defaultMaxLines = 2000
	defaultMaxBytes = 50 * 1024
	// Bytes of the selection kept in memory: enough to fill the output budget several times over,
	// because truncateHeadOf never returns more than defaultMaxBytes of text.
	keepBytes = 4 * defaultMaxBytes
	readChunk = 64 * 1024
	// The agent passes its model's image limits; these are pi-durable's defaults for the gap.
	defaultImageBytes  = 5 * 1024 * 1024
	defaultImageWidth  = 8000
	defaultImageHeight = 8000
)

// ReadArgs mirrors the `read` tool's parameters plus the model facts the agent knows and the
// file does not: whether the model takes images and the size limits it accepts.
type ReadArgs struct {
	Path           string   `json:"path"`
	Offset         *float64 `json:"offset,omitempty"`
	Limit          *float64 `json:"limit,omitempty"`
	Vision         *bool    `json:"vision,omitempty"`
	MaxImageBytes  *int     `json:"maxImageBytes,omitempty"`
	MaxImageWidth  *int     `json:"maxImageWidth,omitempty"`
	MaxImageHeight *int     `json:"maxImageHeight,omitempty"`
}

// ReadDiagnostic is one note about the result, in the agent's own vocabulary.
type ReadDiagnostic struct {
	Severity string `json:"severity"`
	Code     string `json:"code,omitempty"`
	Message  string `json:"message"`
}

// ReadTruncation mirrors the fields pi-durable reports under details.truncation.
type ReadTruncation struct {
	Truncated             bool `json:"truncated"`
	TruncatedBy           any  `json:"truncatedBy"`
	TotalLines            int  `json:"totalLines"`
	TotalBytes            int  `json:"totalBytes"`
	OutputLines           int  `json:"outputLines"`
	OutputBytes           int  `json:"outputBytes"`
	LastLinePartial       bool `json:"lastLinePartial"`
	FirstLineExceedsLimit bool `json:"firstLineExceedsLimit"`
	MaxLines              int  `json:"maxLines"`
	MaxBytes              int  `json:"maxBytes"`
}

// ReadDetails is present only when the output had to be cut.
type ReadDetails struct {
	Truncation ReadTruncation `json:"truncation"`
}

// ReadImage is one image content block, base64 encoded exactly as read from disk.
type ReadImage struct {
	Type     string `json:"type"`
	Data     string `json:"data"`
	MimeType string `json:"mimeType"`
}

// ReadResult is what the agent turns into text or image output.
type ReadResult struct {
	Text        string           `json:"text"`
	Image       *ReadImage       `json:"image,omitempty"`
	Diagnostics []ReadDiagnostic `json:"diagnostics,omitempty"`
	Details     *ReadDetails     `json:"details,omitempty"`
	IsError     bool             `json:"isError,omitempty"`
}

// RegisterRead adds the `read` tool: the file is opened, bounded, and described here.
func RegisterRead(srv *server.Server) {
	srv.Register("read", func(ctx context.Context, req proto.Request, _ server.Emit) (any, error) {
		var args ReadArgs
		if err := json.Unmarshal(req.Args, &args); err != nil {
			return nil, fmt.Errorf("Invalid read arguments: %v", err)
		}
		absolutePath := resolvePath(req.Cwd, args.Path)
		started := time.Now()
		result, err := readFileTool(absolutePath, args)
		if err != nil {
			srv.Logf("read: path=%s failed=%q took=%s", args.Path, err.Error(), time.Since(started).Round(time.Millisecond))
			return nil, err
		}
		srv.Logf("read: path=%s bytes=%d image=%t truncated=%t took=%s",
			args.Path, len(result.Text), result.Image != nil, result.Details != nil, time.Since(started).Round(time.Millisecond))
		return result, nil
	})
}

// readFileTool keeps the path as given in its messages; only the file system call is resolved.
func readFileTool(absolutePath string, args ReadArgs) (ReadResult, error) {
	resolved := resolveReadPath(absolutePath)
	// A concurrent writer can change the file between the read and the check. Appending leaves the
	// text as it was; an image must be whole. Anything else is read once more, then reported.
	for attempt := 0; ; attempt++ {
		before, statErr := os.Stat(resolved)
		mime, err := sniffImage(resolved)
		if err != nil {
			// pi-durable lets the fs error reach the model as-is: a directory reads without a path,
			// a missing file names it.
			return ReadResult{}, nodeReadError(err, resolved)
		}
		var result ReadResult
		if mime != "" {
			// An image is handed over whole, so it is the one case that still loads the file.
			raw, readErr := os.ReadFile(resolved)
			if readErr != nil {
				return ReadResult{}, nodeReadError(readErr, resolved)
			}
			result, err = readImage(args.Path, raw, mime, args)
		} else {
			result, err = readText(args.Path, resolved, args)
		}
		if err != nil || statErr != nil {
			return result, err
		}
		after, afterErr := os.Stat(resolved)
		if afterErr != nil {
			return result, nil
		}
		unchanged := after.Size() == before.Size() && after.ModTime().Equal(before.ModTime())
		if unchanged || (mime == "" && after.Size() > before.Size()) {
			return result, nil
		}
		if attempt == 1 {
			return ReadResult{}, fmt.Errorf("%s changed while it was read", args.Path)
		}
	}
}

// sniffImage names the format from the file's header and, for a png, reads the chunk headers only:
// an animated png hides its acTL chunk behind chunks of any size, so a fixed prefix cannot tell.
func sniffImage(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	header := make([]byte, 32)
	read, err := io.ReadFull(file, header)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return "", err
	}
	header = header[:read]
	if !hasPrefix(header, pngSignature) {
		return detectImageMimeType(header), nil
	}
	if !isPng(header) {
		return "", nil
	}
	animated, err := animatedPngFile(file)
	if err != nil {
		return "", err
	}
	if animated {
		return "", nil
	}
	return "image/png", nil
}

// animatedPngFile walks the chunk headers with reads of eight bytes each.
func animatedPngFile(file *os.File) (bool, error) {
	offset := int64(len(pngSignature))
	chunk := make([]byte, 8)
	for {
		if _, err := file.ReadAt(chunk, offset); err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				return false, nil
			}
			return false, err
		}
		length := int64(binary.BigEndian.Uint32(chunk[:4]))
		kind := string(chunk[4:8])
		switch kind {
		case "acTL":
			return true, nil
		case "IDAT":
			return false, nil
		}
		next := offset + 8 + length + 4
		if next <= offset {
			return false, nil
		}
		offset = next
	}
}

// readText mirrors pi-durable's readText, including the way it treats a fractional or zero offset:
// the schema only promises numbers, so the arithmetic and the messages have to follow JavaScript.
// It scans the file instead of loading it, so the cost is bounded by what the answer needs.
func readText(displayPath, resolved string, args ReadArgs) (ReadResult, error) {
	offset := 0.0
	if args.Offset != nil {
		offset = *args.Offset
	}
	startLine := 0.0
	if offset != 0 {
		startLine = math.Max(0, offset-1)
	}
	startLineDisplay := startLine + 1
	sliceStart := sliceIndex(startLine)
	totalFileLines, err := countFileLines(resolved)
	if err != nil {
		return ReadResult{}, nodeReadError(err, resolved)
	}
	if startLine >= float64(totalFileLines) {
		return ReadResult{}, fmt.Errorf("Offset %s is beyond end of file (%d lines total)", jsNumber(offset), totalFileLines)
	}
	selectedLineCount := totalFileLines - sliceStart
	var userLimitedLines *float64
	if args.Limit != nil {
		endLine := math.Min(startLine+*args.Limit, float64(totalFileLines))
		limited := endLine - startLine
		userLimitedLines = &limited
		relativeEnd := sliceIndex(endLine)
		sliceEnd := relativeEnd
		if relativeEnd < 0 {
			sliceEnd = maxInt(0, totalFileLines+relativeEnd)
		}
		selectedLineCount = maxInt(0, sliceEnd-sliceStart)
	}
	empty := selectedLineCount == 0
	// The selected text is the lines joined with "\n", and its byte length is what the tool reports.
	selection := lineSelection{}
	if !empty {
		selection, err = selectLines(resolved, sliceStart, sliceStart+selectedLineCount, keepBytes)
		if err != nil {
			return ReadResult{}, nodeReadError(err, resolved)
		}
	}
	head := ""
	if !empty {
		head = decodeFileUTF8(selection.kept)
		if sliceStart == 0 {
			// The reference starts at byte 3 when the file opens with a mark, so it never shows one.
			head = strings.TrimPrefix(head, "\ufeff")
		}
	}
	selectedBytes := selection.bytes
	firstLineBytes := selection.firstLineBytes
	endsWithNewline := selection.endsWithNewline
	totalLines := 0
	if !empty && selectedBytes != 0 {
		totalLines = selectedLineCount
		if endsWithNewline {
			totalLines--
		}
	}
	totals := truncateTotals{lines: totalLines, bytes: selectedBytes}
	if empty {
		totals = truncateTotals{}
	}
	content, truncation := truncateHeadOf(head, totals)
	var diagnostics []ReadDiagnostic
	outputText := content
	var details *ReadDetails
	switch {
	case truncation.FirstLineExceedsLimit:
		// A fractional start line names no line, so nothing of it is shown.
		integral := startLine == math.Trunc(startLine)
		lineText := ""
		lineSize := 0
		if integral {
			lineText = strings.SplitN(head, "\n", 2)[0]
			lineSize = firstLineBytes
		}
		end := characterEnd(lineText, defaultMaxBytes)
		outputText = lineText[:end]
		diagnostics = append(diagnostics, ReadDiagnostic{
			Severity: "warn",
			Code:     "truncated",
			Message: fmt.Sprintf("Line %s is %s, exceeds the %s limit; showing its first %s. Use bash: sed -n '%dp' %s | tail -c +%d",
				jsNumber(startLineDisplay), formatSize(lineSize), formatSize(defaultMaxBytes), formatSize(end),
				int(startLineDisplay), displayPath, end+1),
		})
		copied := truncation
		copied.OutputBytes = end
		copied.OutputLines = 1
		details = &ReadDetails{Truncation: copied}
	case truncation.Truncated:
		endLineDisplay := startLineDisplay + float64(truncation.OutputLines) - 1
		nextOffset := endLineDisplay + 1
		limitText := ""
		if truncation.TruncatedBy != "lines" {
			limitText = fmt.Sprintf(" (%s limit)", formatSize(defaultMaxBytes))
		}
		diagnostics = append(diagnostics, ReadDiagnostic{
			Severity: "info",
			Code:     "truncated",
			Message: fmt.Sprintf("Showing lines %s-%s of %d%s. Use offset=%s to continue.",
				jsNumber(startLineDisplay), jsNumber(endLineDisplay), totalFileLines, limitText, jsNumber(nextOffset)),
		})
		details = &ReadDetails{Truncation: truncation}
	case userLimitedLines != nil && startLine+*userLimitedLines < float64(totalFileLines):
		remaining := float64(totalFileLines) - (startLine + *userLimitedLines)
		nextOffset := startLine + *userLimitedLines + 1
		diagnostics = append(diagnostics, ReadDiagnostic{
			Severity: "info",
			Message:  fmt.Sprintf("%s more lines in file. Use offset=%s to continue.", jsNumber(remaining), jsNumber(nextOffset)),
		})
	}
	return ReadResult{Text: outputText, Diagnostics: diagnostics, Details: details}, nil
}

// sliceIndex is Array.prototype.slice's index conversion: NaN is 0, everything else truncates toward zero.
func sliceIndex(value float64) int {
	if math.IsNaN(value) {
		return 0
	}
	return int(math.Trunc(value))
}

// jsNumber formats a number the way a JavaScript template literal would.
func jsNumber(value float64) string {
	if value == math.Trunc(value) && math.Abs(value) < 1e21 {
		return strconv.FormatInt(int64(value), 10)
	}
	return strconv.FormatFloat(value, 'g', -1, 64)
}

// splitFileLines splits like the tool does: on "\n" only, so a CRLF file keeps its "\r" in the
// line text, and a trailing newline produces a final empty line.
// countFileLines counts the lines a split on "\n" would produce, without loading the file.
func countFileLines(path string) (int, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer file.Close()
	chunk := make([]byte, readChunk)
	newlines := 0
	for {
		read, err := file.Read(chunk)
		for _, b := range chunk[:read] {
			if b == '\n' {
				newlines++
			}
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return 0, err
		}
	}
	return newlines + 1, nil
}

// lineSelection is what the answer needs to know about the selected lines, plus a bounded prefix
// of their text; the rest of the selection is counted and dropped.
type lineSelection struct {
	lines           int
	bytes           int
	firstLineBytes  int
	endsWithNewline bool
	kept            []byte
}

// selectLines streams the file up to the end of the selection, counting every selected line and
// keeping only the first keep bytes of their text.
func selectLines(path string, start, end, keep int) (lineSelection, error) {
	file, err := os.Open(path)
	if err != nil {
		return lineSelection{}, err
	}
	defer file.Close()
	selection := lineSelection{}
	chunk := make([]byte, readChunk)
	line := 0
	var content []byte
	keptBytes := 0
	record := func(content []byte, final bool) {
		if line >= start && line < end {
			if selection.lines == 0 {
				selection.firstLineBytes = len(content)
			} else {
				selection.bytes++
			}
			selection.lines++
			selection.bytes += len(content)
			if keptBytes < keep {
				if len(selection.kept) > 0 {
					selection.kept = append(selection.kept, '\n')
					keptBytes++
				}
				selection.kept = append(selection.kept, content...)
				keptBytes += len(content)
			}
			// The last line of a file that ends with a newline is empty and adds no line to the count.
			if final && len(content) == 0 && line == end-1 {
				selection.endsWithNewline = true
			}
		}
		line++
	}
	for line < end {
		read, err := file.Read(chunk)
		for _, b := range chunk[:read] {
			if b == '\n' {
				record(content, false)
				content = content[:0]
				continue
			}
			content = append(content, b)
		}
		if errors.Is(err, io.EOF) {
			record(content, true)
			break
		}
		if err != nil {
			return lineSelection{}, err
		}
	}
	return selection, nil
}

type truncateTotals struct {
	lines int
	bytes int
}

// truncateHeadOf is pi-durable's truncateHeadOf: whole lines only, line and byte limits racing.
func truncateHeadOf(prefix string, totals truncateTotals) (string, ReadTruncation) {
	maxLines, maxBytes := defaultMaxLines, defaultMaxBytes
	totalLines, totalBytes := totals.lines, totals.bytes
	lines := splitLinesForCounting(prefix)
	result := ReadTruncation{
		TruncatedBy: nil,
		TotalLines:  totalLines,
		TotalBytes:  totalBytes,
		MaxLines:    maxLines,
		MaxBytes:    maxBytes,
	}
	if totalLines <= maxLines && totalBytes <= maxBytes {
		result.OutputLines = totalLines
		result.OutputBytes = totalBytes
		return prefix, result
	}
	firstLineBytes := 0
	if len(lines) > 0 {
		firstLineBytes = len(lines[0])
	}
	if firstLineBytes > maxBytes {
		result.Truncated = true
		result.TruncatedBy = "bytes"
		result.FirstLineExceedsLimit = true
		return "", result
	}
	var kept []string
	outputBytes := 0
	truncatedBy := "lines"
	for i := 0; i < len(lines) && i < maxLines; i++ {
		lineBytes := len(lines[i])
		if i > 0 {
			lineBytes++
		}
		if outputBytes+lineBytes > maxBytes {
			truncatedBy = "bytes"
			break
		}
		kept = append(kept, lines[i])
		outputBytes += lineBytes
	}
	if truncatedBy != "bytes" && len(kept) >= totalLines {
		truncatedBy = "bytes"
	}
	result.Truncated = true
	result.TruncatedBy = truncatedBy
	result.OutputLines = len(kept)
	content := strings.Join(kept, "\n")
	result.OutputBytes = len(content)
	return content, result
}

func splitLinesForCounting(content string) []string {
	if content == "" {
		return nil
	}
	lines := strings.Split(content, "\n")
	if strings.HasSuffix(content, "\n") {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// characterEnd cuts bytes at the byte limit without splitting a UTF-8 sequence.
func characterEnd(text string, limit int) int {
	if len(text) <= limit {
		return len(text)
	}
	end := limit
	for end > 0 && (text[end]&0xC0) == 0x80 {
		end--
	}
	return end
}

func formatSize(bytes int) string {
	switch {
	case bytes < 1024:
		return strconv.Itoa(bytes) + "B"
	case bytes < 1024*1024:
		return fmt.Sprintf("%.1fKB", float64(bytes)/1024)
	default:
		return fmt.Sprintf("%.1fMB", float64(bytes)/(1024*1024))
	}
}

// detectImageMimeType is pi-durable's detectSupportedImageMimeType: a signature alone is not enough
// for PNG (it must be a still, well-formed one) or BMP (its header must parse), and an APNG or a
// malformed header reads as text instead of an image.
func detectImageMimeType(data []byte) string {
	switch {
	case len(data) >= 4 && data[0] == 0xFF && data[1] == 0xD8 && data[2] == 0xFF:
		// A fourth byte of 0xF7 marks a JPEG this tool cannot describe; Node reads it as text.
		if data[3] == 0xF7 {
			return ""
		}
		return "image/jpeg"
	case hasPrefix(data, pngSignature):
		if isPng(data) && !isAnimatedPng(data) {
			return "image/png"
		}
		return ""
	case hasPrefix(data, []byte("GIF87a")) || hasPrefix(data, []byte("GIF89a")):
		return "image/gif"
	case hasPrefix(data, []byte("RIFF")) && len(data) >= 12 && string(data[8:12]) == "WEBP":
		return "image/webp"
	case hasPrefix(data, []byte("BM")) && isBmp(data):
		// Detected, but not a format this tool can send inline; readImage explains that.
		return "image/bmp"
	}
	return ""
}

var pngSignature = []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}

func hasPrefix(data, prefix []byte) bool {
	return len(data) >= len(prefix) && string(data[:len(prefix)]) == string(prefix)
}

// isPng requires the signature, the 13-byte IHDR as the first chunk, and the 16 header bytes.
func isPng(data []byte) bool {
	return len(data) >= 16 &&
		hasPrefix(data, pngSignature) &&
		binary.BigEndian.Uint32(data[8:12]) == 13 &&
		string(data[12:16]) == "IHDR"
}

// isAnimatedPng walks the chunks until acTL (animated) or IDAT (the still image starts).
func isAnimatedPng(data []byte) bool {
	offset := len(pngSignature)
	for offset+8 <= len(data) {
		chunkLength := int64(binary.BigEndian.Uint32(data[offset : offset+4]))
		chunkType := string(data[offset+4 : offset+8])
		if chunkType == "acTL" {
			return true
		}
		if chunkType == "IDAT" {
			return false
		}
		nextOffset := int64(offset) + 8 + chunkLength + 4
		if nextOffset <= int64(offset) || nextOffset > int64(len(data)) {
			return false
		}
		offset = int(nextOffset)
	}
	return false
}

// isBmp validates the bitmap header fields Node checks before calling a file a BMP.
func isBmp(data []byte) bool {
	if len(data) < 26 {
		return false
	}
	declaredFileSize := binary.LittleEndian.Uint32(data[2:6])
	pixelDataOffset := binary.LittleEndian.Uint32(data[10:14])
	dibHeaderSize := binary.LittleEndian.Uint32(data[14:18])
	if declaredFileSize != 0 && declaredFileSize < 26 {
		return false
	}
	if int64(pixelDataOffset) < int64(14+dibHeaderSize) {
		return false
	}
	if declaredFileSize != 0 && pixelDataOffset >= declaredFileSize {
		return false
	}
	var colorPlanes, bitsPerPixel uint16
	switch {
	case dibHeaderSize == 12:
		colorPlanes = binary.LittleEndian.Uint16(data[22:24])
		bitsPerPixel = binary.LittleEndian.Uint16(data[24:26])
	case dibHeaderSize >= 40 && dibHeaderSize <= 124:
		if len(data) < 30 {
			return false
		}
		colorPlanes = binary.LittleEndian.Uint16(data[26:28])
		bitsPerPixel = binary.LittleEndian.Uint16(data[28:30])
	default:
		return false
	}
	if colorPlanes != 1 {
		return false
	}
	switch bitsPerPixel {
	case 1, 4, 8, 16, 24, 32:
		return true
	}
	return false
}

func readImage(displayPath string, data []byte, mimeType string, args ReadArgs) (ReadResult, error) {
	maxBytes := defaultImageBytes
	if args.MaxImageBytes != nil {
		maxBytes = *args.MaxImageBytes
	}
	maxWidth, maxHeight := defaultImageWidth, defaultImageHeight
	if args.MaxImageWidth != nil {
		maxWidth = *args.MaxImageWidth
	}
	if args.MaxImageHeight != nil {
		maxHeight = *args.MaxImageHeight
	}
	vision := args.Vision == nil || *args.Vision
	if !inlineImageTypes[mimeType] {
		// No image processor is configured here, so a BMP cannot be converted for the model.
		return imageError(fmt.Sprintf("%s is an image (%s) that needs converting, and no image processor is configured", displayPath, mimeType)), nil
	}
	unshrinkable := "and no image processor is configured to shrink it"
	if base64Length(len(data)) > maxBytes {
		return imageError(fmt.Sprintf("%s is an image (%s) of %s, too large to send, %s", displayPath, mimeType, formatSize(len(data)), unshrinkable)), nil
	}
	width, height, ok := imageDimensions(data, mimeType)
	if !ok {
		return imageError(fmt.Sprintf("%s is an image (%s) whose size cannot be read", displayPath, mimeType)), nil
	}
	if width > maxWidth || height > maxHeight {
		return imageError(fmt.Sprintf("%s is an image (%s) of %dx%d, larger than %dx%d, %s",
			displayPath, mimeType, width, height, maxWidth, maxHeight, unshrinkable)), nil
	}
	notes := []string{fmt.Sprintf("Read image file [%s].", mimeType)}
	if !vision {
		notes = append(notes, "The current model does not support images; it sees a placeholder instead.")
	}
	return ReadResult{
		Image: &ReadImage{Type: "image", Data: base64.StdEncoding.EncodeToString(data), MimeType: mimeType},
		Diagnostics: []ReadDiagnostic{{
			Severity: "info",
			Code:     "image",
			Message:  strings.Join(notes, " "),
		}},
	}, nil
}

// inlineImageTypes are the formats pi-durable sends without conversion.
var inlineImageTypes = map[string]bool{
	"image/png":  true,
	"image/jpeg": true,
	"image/gif":  true,
	"image/webp": true,
}

func imageError(message string) ReadResult {
	return ReadResult{
		IsError:     true,
		Diagnostics: []ReadDiagnostic{{Severity: "error", Code: "unsupported_image", Message: message}},
	}
}

// base64Length is the encoded length of size bytes, computed without encoding them.
func base64Length(size int) int {
	return (size + 2) / 3 * 4
}

func imageDimensions(data []byte, mimeType string) (int, int, bool) {
	switch mimeType {
	case "image/png":
		if len(data) < 24 {
			return 0, 0, false
		}
		return int(binary.BigEndian.Uint32(data[16:20])), int(binary.BigEndian.Uint32(data[20:24])), true
	case "image/gif":
		if len(data) < 10 {
			return 0, 0, false
		}
		return int(binary.LittleEndian.Uint16(data[6:8])), int(binary.LittleEndian.Uint16(data[8:10])), true
	case "image/webp":
		return webpDimensions(data)
	case "image/jpeg":
		return jpegDimensions(data)
	}
	return 0, 0, false
}

func webpDimensions(data []byte) (int, int, bool) {
	if len(data) < 30 {
		return 0, 0, false
	}
	chunk := string(data[12:16])
	switch chunk {
	case "VP8X":
		width := int(uint32(data[24])|uint32(data[25])<<8|uint32(data[26])<<16) + 1
		height := int(uint32(data[27])|uint32(data[28])<<8|uint32(data[29])<<16) + 1
		return width, height, true
	case "VP8 ":
		if len(data) < 30 {
			return 0, 0, false
		}
		width := int(binary.LittleEndian.Uint16(data[26:28]) & 0x3FFF)
		height := int(binary.LittleEndian.Uint16(data[28:30]) & 0x3FFF)
		return width, height, true
	case "VP8L":
		if len(data) < 25 {
			return 0, 0, false
		}
		bits := binary.LittleEndian.Uint32(data[21:25])
		width := int(bits&0x3FFF) + 1
		height := int((bits>>14)&0x3FFF) + 1
		return width, height, true
	}
	return 0, 0, false
}

// jpegDimensions walks the segment markers until a start-of-frame carries the size.
func jpegDimensions(data []byte) (int, int, bool) {
	i := 2
	for i+9 < len(data) {
		if data[i] != 0xFF {
			i++
			continue
		}
		marker := data[i+1]
		if marker == 0xD8 || marker == 0x01 || (marker >= 0xD0 && marker <= 0xD7) {
			i += 2
			continue
		}
		if i+4 > len(data) {
			return 0, 0, false
		}
		length := int(binary.BigEndian.Uint16(data[i+2 : i+4]))
		isSOF := marker >= 0xC0 && marker <= 0xCF && marker != 0xC4 && marker != 0xC8 && marker != 0xCC
		if isSOF {
			if i+9 > len(data) {
				return 0, 0, false
			}
			height := int(binary.BigEndian.Uint16(data[i+5 : i+7]))
			width := int(binary.BigEndian.Uint16(data[i+7 : i+9]))
			return width, height, true
		}
		i += 2 + length
	}
	return 0, 0, false
}
