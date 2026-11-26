package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"pilot/toolbox/internal/proto"
	"pilot/toolbox/internal/server"
)

// Edit is one replacement: exact text to find, and what to put in its place.
type Edit struct {
	OldText string `json:"oldText"`
	NewText string `json:"newText"`
}

// EditArgs mirrors the `edit` tool's parameters after the agent repaired the shapes models send.
type EditArgs struct {
	Path  string `json:"path"`
	Edits []Edit `json:"edits"`
}

// EditResult carries the facts the agent cannot compute without doing the edit again: the texts
// the diff is built from, already in the shape the tool reports them.
type EditResult struct {
	Text             string `json:"text"`
	Diff             string `json:"diff"`
	Patch            string `json:"patch"`
	FirstChangedLine *int   `json:"firstChangedLine,omitempty"`
}

// RegisterEdit adds the `edit` tool: matching, line endings, and the write all happen here.
func RegisterEdit(srv *server.Server) {
	srv.Register("edit", func(ctx context.Context, req proto.Request, _ server.Emit) (any, error) {
		return runEdit(srv, ctx, req)
	})
}

func runEdit(srv *server.Server, ctx context.Context, req proto.Request) (any, error) {
	var args EditArgs
	if err := json.Unmarshal(req.Args, &args); err != nil {
		return nil, fmt.Errorf("Invalid edit arguments: %v", err)
	}
	if len(args.Edits) == 0 {
		return nil, fmt.Errorf("Edit tool input is invalid. edits must contain at least one replacement.")
	}
	absolutePath := resolvePath(req.Cwd, args.Path)
	started := time.Now()
	var result EditResult
	// Serialize with `write` on the same file: the two are read-modify-write and would otherwise race.
	err := withFileMutation(absolutePath, func() error {
		var applyErr error
		result, applyErr = applyEdit(ctx, absolutePath, args.Path, args.Edits)
		return applyErr
	})
	if err != nil {
		srv.Logf("edit: path=%s failed=%q took=%s", args.Path, err.Error(), time.Since(started).Round(time.Millisecond))
		return nil, err
	}
	srv.Logf("edit: path=%s edits=%d took=%s", args.Path, len(args.Edits), time.Since(started).Round(time.Millisecond))
	return result, nil
}

// applyEdit follows pi-durable's edit tool step for step so an error reads the same.
func applyEdit(ctx context.Context, absolutePath, displayPath string, edits []Edit) (EditResult, error) {
	if err := ctx.Err(); err != nil {
		return EditResult{}, fmt.Errorf("Operation aborted")
	}
	info, statErr := os.Stat(absolutePath)
	if statErr != nil {
		return EditResult{}, fmt.Errorf("Could not edit file: %s. Error code: %s.", displayPath, fileErrorCode(statErr))
	}
	if !info.Mode().IsRegular() && !isSymlink(absolutePath) {
		return EditResult{}, fmt.Errorf("Could not edit file: %s. Path is not a file.", displayPath)
	}
	raw, readErr := os.ReadFile(absolutePath)
	if readErr != nil {
		return EditResult{}, fmt.Errorf("Could not edit file: %s. Error code: %s.", displayPath, fileErrorCode(readErr))
	}
	if err := ctx.Err(); err != nil {
		return EditResult{}, fmt.Errorf("Operation aborted")
	}
	bom, content := stripBom(decodeUTF8(raw))
	originalEnding := detectLineEnding(content)
	baseContent, newContent, err := applyEditsToNormalizedContent(normalizeToLF(content), edits, displayPath)
	if err != nil {
		return EditResult{}, err
	}
	if err := ctx.Err(); err != nil {
		return EditResult{}, fmt.Errorf("Operation aborted")
	}
	finalContent := bom + restoreLineEndings(newContent, originalEnding)
	if err := os.MkdirAll(filepath.Dir(absolutePath), 0o755); err != nil {
		return EditResult{}, fmt.Errorf("Could not edit file: %s. Error code: %s.", displayPath, fileErrorCode(err))
	}
	if writeErr := os.WriteFile(absolutePath, []byte(finalContent), 0o644); writeErr != nil {
		return EditResult{}, fmt.Errorf("Could not edit file: %s. Error code: %s.", displayPath, fileErrorCode(writeErr))
	}
	if err := ctx.Err(); err != nil {
		return EditResult{}, fmt.Errorf("Operation aborted")
	}
	diff, firstChangedLine := generateDiffString(baseContent, newContent, 4)
	return EditResult{
		Text:             fmt.Sprintf("Successfully replaced %d block(s) in %s.", len(edits), displayPath),
		Diff:             diff,
		Patch:            generateUnifiedPatch(displayPath, baseContent, newContent, 4),
		FirstChangedLine: firstChangedLine,
	}, nil
}

func isSymlink(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.Mode()&os.ModeSymlink != 0
}

// WriteArgs mirrors the `write` tool's parameters.
type WriteArgs struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// WriteResult is the tool's message; the agent renders it as the tool's output text.
type WriteResult struct {
	Text string `json:"text"`
}

// RegisterWrite adds the `write` tool: it creates parent directories, then replaces the file.
func RegisterWrite(srv *server.Server) {
	srv.Register("write", func(ctx context.Context, req proto.Request, _ server.Emit) (any, error) {
		var args WriteArgs
		if err := json.Unmarshal(req.Args, &args); err != nil {
			return nil, fmt.Errorf("Invalid write arguments: %v", err)
		}
		absolutePath := resolvePath(req.Cwd, args.Path)
		started := time.Now()
		if err := withFileMutation(absolutePath, func() error { return writeFile(ctx, absolutePath, args.Content) }); err != nil {
			srv.Logf("write: path=%s failed=%q took=%s", args.Path, err.Error(), time.Since(started).Round(time.Millisecond))
			return nil, err
		}
		srv.Logf("write: path=%s bytes=%d took=%s", args.Path, len(args.Content), time.Since(started).Round(time.Millisecond))
		return WriteResult{Text: fmt.Sprintf("Successfully wrote to %s", args.Path)}, nil
	})
}

func writeFile(ctx context.Context, absolutePath, content string) error {
	if ctx.Err() != nil {
		return fmt.Errorf("Operation aborted")
	}
	if err := os.MkdirAll(filepath.Dir(absolutePath), 0o755); err != nil {
		return nodeWriteError(err, absolutePath)
	}
	if ctx.Err() != nil {
		return fmt.Errorf("Operation aborted")
	}
	if err := os.WriteFile(absolutePath, []byte(content), 0o644); err != nil {
		return nodeWriteError(err, absolutePath)
	}
	if ctx.Err() != nil {
		return fmt.Errorf("Operation aborted")
	}
	return nil
}
