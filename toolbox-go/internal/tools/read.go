package tools

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
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
	// The agent passes its model's image limits; these are pi-durable's defaults for the gap.
	defaultImageBytes  = 5 * 1024 * 1024
	defaultImageWidth  = 8000
	defaultImageHeight = 8000
)

// ReadArgs mirrors the `read` tool's parameters plus the model facts the agent knows and the
// file does not: whether the model takes images and the size limits it accepts.
type ReadArgs struct {
	Path           string `json:"path"`
	Offset         *int   `json:"offset,omitempty"`
	Limit          *int   `json:"limit,omitempty"`
	Vision         *bool  `json:"vision,omitempty"`
	MaxImageBytes  *int   `json:"maxImageBytes,omitempty"`
	MaxImageWidth  *int   `json:"maxImageWidth,omitempty"`
	MaxImageHeight *int   `json:"maxImageHeight,omitempty"`
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

func readFileTool(absolutePath string, args ReadArgs) (ReadResult, error) {
	raw, err := os.ReadFile(absolutePath)
	if err != nil {
		// pi-durable lets the fs error reach the model as-is, path and all.
		return ReadResult{}, nodeStyleFileError(err, absolutePath)
	}
	if mime := detectImageMimeType(raw); mime != "" {
		return readImage(args.Path, raw, mime, args)
	}
	return readText(args.Path, decodeFileUTF8(raw), args)
}

func readText(displayPath, text string, args ReadArgs) (ReadResult, error) {
	offset := 0
	if args.Offset != nil {
		offset = *args.Offset
	}
	startLine := 0
	if offset != 0 {
		startLine = offset - 1
		if startLine < 0 {
			startLine = 0
		}
	}
	lines := splitFileLines(text)
	totalFileLines := len(lines)
	if startLine >= totalFileLines {
		return ReadResult{}, fmt.Errorf("Offset %d is beyond end of file (%d lines total)", offset, totalFileLines)
	}
	selectedLineCount := totalFileLines - startLine
	var userLimitedLines *int
	if args.Limit != nil {
		endLine := startLine + *args.Limit
		if endLine > totalFileLines {
			endLine = totalFileLines
		}
		if endLine < startLine {
			endLine = startLine
		}
		limited := endLine - startLine
		userLimitedLines = &limited
		selectedLineCount = limited
	}
	empty := selectedLineCount == 0
	selected := []string(nil)
	if !empty {
		selected = lines[startLine : startLine+selectedLineCount]
	}
	// scan.end is the end of the last selected line without its terminator, so the joined text is
	// exactly the scanned range and its byte length is what the tool reports.
	head := ""
	if !empty {
		head = strings.Join(selected, "\n")
	}
	selectedBytes := len(head)
	firstLineBytes := 0
	if len(selected) > 0 {
		firstLineBytes = len(selected[0])
	}
	endsWithNewline := len(selected) > 1 && selected[len(selected)-1] == ""
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
		integral := args.Offset == nil || *args.Offset != 0
		lineBytes := ""
		if integral {
			lineBytes = strings.SplitN(head, "\n", 2)[0]
		}
		lineSize := 0
		if integral {
			lineSize = firstLineBytes
		}
		end := characterEnd(lineBytes, defaultMaxBytes)
		outputText = lineBytes[:end]
		diagnostics = append(diagnostics, ReadDiagnostic{
			Severity: "warn",
			Code:     "truncated",
			Message: fmt.Sprintf("Line %d is %s, exceeds the %s limit; showing its first %s. Use bash: sed -n '%dp' %s | tail -c +%d",
				startLine+1, formatSize(lineSize), formatSize(defaultMaxBytes), formatSize(end), startLine+1, displayPath, end+1),
		})
		copied := truncation
		copied.OutputBytes = end
		copied.OutputLines = 1
		details = &ReadDetails{Truncation: copied}
	case truncation.Truncated:
		endLineDisplay := startLine + 1 + truncation.OutputLines - 1
		nextOffset := endLineDisplay + 1
		limitText := ""
		if truncation.TruncatedBy != "lines" {
			limitText = fmt.Sprintf(" (%s limit)", formatSize(defaultMaxBytes))
		}
		diagnostics = append(diagnostics, ReadDiagnostic{
			Severity: "info",
			Code:     "truncated",
			Message: fmt.Sprintf("Showing lines %d-%d of %d%s. Use offset=%d to continue.",
				startLine+1, endLineDisplay, totalFileLines, limitText, nextOffset),
		})
		details = &ReadDetails{Truncation: truncation}
	case userLimitedLines != nil && startLine+*userLimitedLines < totalFileLines:
		remaining := totalFileLines - (startLine + *userLimitedLines)
		nextOffset := startLine + *userLimitedLines + 1
		diagnostics = append(diagnostics, ReadDiagnostic{
			Severity: "info",
			Message:  fmt.Sprintf("%d more lines in file. Use offset=%d to continue.", remaining, nextOffset),
		})
	}
	return ReadResult{Text: outputText, Diagnostics: diagnostics, Details: details}, nil
}

// splitFileLines splits like the tool does: on "\n" only, so a CRLF file keeps its "\r" in the
// line text, and a trailing newline produces a final empty line.
func splitFileLines(text string) []string {
	return strings.Split(text, "\n")
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

func detectImageMimeType(data []byte) string {
	switch {
	case len(data) > 8 && data[0] == 0x89 && string(data[1:4]) == "PNG":
		return "image/png"
	case len(data) > 3 && data[0] == 0xFF && data[1] == 0xD8 && data[2] == 0xFF:
		return "image/jpeg"
	case len(data) > 6 && (string(data[:6]) == "GIF87a" || string(data[:6]) == "GIF89a"):
		return "image/gif"
	case len(data) > 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WEBP":
		return "image/webp"
	}
	return ""
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
