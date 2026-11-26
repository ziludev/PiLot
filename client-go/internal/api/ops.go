package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"pilot/internal/protocol"
)

// Delta ops from the bridge's `?ops=1` watch stream.
//
// The tuples are chord's in-memory `Op` (dist/delta/index.d.ts), not `WireOp`:
// paths are inline, with no path dictionary and no `["#", id, path]` definitions.
// The applier mirrors dist/delta/index.js:300-380, including the two ops whose
// names mislead:
//
//	["a", path, text]  append text to a STRING (streamed text, not a new key)
//	["t", path, n]     keep the string from n on (s.slice(n), not an array cut)
//
// Arrays grow and shrink through ["p", path, start, deleteCount, items] only.
//
// Every mismatch is an error. Silently skipping a bad segment would leave the
// tree subtly misaligned, which is far harder to notice than a reconnect.

// opSegment is one decoded path part: an object key or an array index.
type opSegment struct {
	key   string
	index int
	isIdx bool
}

func (s opSegment) String() string {
	if s.isIdx {
		return strconv.Itoa(s.index)
	}
	return strconv.Quote(s.key)
}

// pathError names the whole path that could not be resolved, so a log line
// points at the frame that broke rather than just at one segment.
type pathError struct {
	path []opSegment
	why  string
}

func (e *pathError) Error() string {
	parts := make([]string, 0, len(e.path))
	for _, seg := range e.path {
		parts = append(parts, seg.String())
	}
	if e.why == "" {
		return fmt.Sprintf("unresolvable path [%s]", strings.Join(parts, " "))
	}
	return fmt.Sprintf("%s at [%s]", e.why, strings.Join(parts, " "))
}

// decodeTree decodes JSON keeping numbers as json.Number: entry data and model
// are re-marshaled verbatim, and a float64 would rewrite IDs beyond 2^53.
func decodeTree(raw json.RawMessage) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var value any
	if err := dec.Decode(&value); err != nil {
		return nil, err
	}
	return value, nil
}

func decodeInto(raw json.RawMessage, dst any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	return dec.Decode(dst)
}

// applyOps applies one batch to the tree and returns the new root, because
// ["r", value] can replace it outright. The input tree may be mutated otherwise.
func applyOps(root any, ops []json.RawMessage) (any, error) {
	for _, raw := range ops {
		var parts []json.RawMessage
		if err := json.Unmarshal(raw, &parts); err != nil {
			return nil, fmt.Errorf("op is not a tuple: %w", err)
		}
		if len(parts) == 0 {
			return nil, fmt.Errorf("op is not a tuple")
		}
		var verb string
		if err := json.Unmarshal(parts[0], &verb); err != nil {
			return nil, fmt.Errorf("op verb is not a string")
		}
		switch verb {
		case "r":
			if len(parts) != 2 {
				return nil, fmt.Errorf("r arity %d", len(parts))
			}
			value, err := decodeTree(parts[1])
			if err != nil {
				return nil, fmt.Errorf("r value: %w", err)
			}
			root = value
		case "s":
			if len(parts) != 3 {
				return nil, fmt.Errorf("s arity %d", len(parts))
			}
			value, err := decodeTree(parts[2])
			if err != nil {
				return nil, fmt.Errorf("s value: %w", err)
			}
			path, err := decodePath(parts[1])
			if err != nil {
				return nil, fmt.Errorf("s path: %w", err)
			}
			next, err := setPath(root, path, value)
			if err != nil {
				return nil, err
			}
			root = next
		case "d":
			if len(parts) != 2 {
				return nil, fmt.Errorf("d arity %d", len(parts))
			}
			path, err := decodePath(parts[1])
			if err != nil {
				return nil, fmt.Errorf("d path: %w", err)
			}
			next, err := deletePath(root, path)
			if err != nil {
				return nil, err
			}
			root = next
		case "a":
			if len(parts) != 3 {
				return nil, fmt.Errorf("a arity %d", len(parts))
			}
			var text string
			if err := json.Unmarshal(parts[2], &text); err != nil {
				return nil, fmt.Errorf("a value is not a string")
			}
			path, err := decodePath(parts[1])
			if err != nil {
				return nil, fmt.Errorf("a path: %w", err)
			}
			next, err := updateString(root, path, func(s string) string { return s + text })
			if err != nil {
				return nil, err
			}
			root = next
		case "t":
			if len(parts) != 3 {
				return nil, fmt.Errorf("t arity %d", len(parts))
			}
			var count int
			if err := json.Unmarshal(parts[2], &count); err != nil || count < 0 {
				return nil, fmt.Errorf("t count is not a non-negative integer")
			}
			path, err := decodePath(parts[1])
			if err != nil {
				return nil, fmt.Errorf("t path: %w", err)
			}
			next, err := updateString(root, path, func(s string) string {
				// s[n:] with n past the end is "", but slicing would panic.
				if count >= len(s) {
					return ""
				}
				return s[count:]
			})
			if err != nil {
				return nil, err
			}
			root = next
		case "p":
			if len(parts) != 5 {
				return nil, fmt.Errorf("p arity %d", len(parts))
			}
			var start, remove int
			if err := json.Unmarshal(parts[2], &start); err != nil || start < 0 {
				return nil, fmt.Errorf("p start is not a non-negative integer")
			}
			if err := json.Unmarshal(parts[3], &remove); err != nil || remove < 0 {
				return nil, fmt.Errorf("p remove is not a non-negative integer")
			}
			var items []any
			if err := decodeInto(parts[4], &items); err != nil {
				return nil, fmt.Errorf("p items: %w", err)
			}
			path, err := decodePath(parts[1])
			if err != nil {
				return nil, fmt.Errorf("p path: %w", err)
			}
			next, err := splicePath(root, path, start, remove, items)
			if err != nil {
				return nil, err
			}
			root = next
		case "m":
			if len(parts) != 3 {
				return nil, fmt.Errorf("m arity %d", len(parts))
			}
			var perm []int
			if err := json.Unmarshal(parts[2], &perm); err != nil {
				return nil, fmt.Errorf("m permutation: %w", err)
			}
			path, err := decodePath(parts[1])
			if err != nil {
				return nil, fmt.Errorf("m path: %w", err)
			}
			next, err := permutePath(root, path, perm)
			if err != nil {
				return nil, err
			}
			root = next
		default:
			// Includes "#": the bridge never interns paths on this stream, so seeing
			// one means the producer changed contract, not that we can decode it.
			return nil, fmt.Errorf("unknown op verb %q", verb)
		}
	}
	return root, nil
}

func decodePath(raw json.RawMessage) ([]opSegment, error) {
	var parts []any
	if err := decodeInto(raw, &parts); err != nil {
		return nil, fmt.Errorf("path is not an array: %w", err)
	}
	path := make([]opSegment, 0, len(parts))
	for _, part := range parts {
		switch value := part.(type) {
		case string:
			if value == "__proto__" || value == "constructor" || value == "prototype" {
				return nil, fmt.Errorf("reserved segment %q", value)
			}
			path = append(path, opSegment{key: value})
		case json.Number:
			index, err := strconv.Atoi(value.String())
			if err != nil || index < 0 {
				return nil, fmt.Errorf("bad path index %q", value.String())
			}
			path = append(path, opSegment{index: index, isIdx: true})
		default:
			return nil, fmt.Errorf("path segment %T is neither a key nor an index", part)
		}
	}
	return path, nil
}

// setPath is ["s"]: parent[key] = value. Objects create the final key; arrays
// may write at index == len (JS grows the array by one) but not past it.
func setPath(node any, path []opSegment, value any) (any, error) {
	if len(path) == 0 {
		// s/d/a/t cannot target the root; only "r" replaces it.
		return nil, &pathError{path: path, why: "op targets the root"}
	}
	seg := path[0]
	switch container := node.(type) {
	case map[string]any:
		if seg.isIdx {
			return nil, &pathError{path: path, why: "index into an object"}
		}
		if len(path) == 1 {
			container[seg.key] = value
			return container, nil
		}
		child, ok := container[seg.key]
		if !ok {
			return nil, &pathError{path: path}
		}
		next, err := setPath(child, path[1:], value)
		if err != nil {
			return nil, err
		}
		container[seg.key] = next
		return container, nil
	case []any:
		if !seg.isIdx {
			return nil, &pathError{path: path, why: "key into an array"}
		}
		if len(path) == 1 {
			if seg.index > len(container) {
				return nil, &pathError{path: path, why: "index past the end"}
			}
			if seg.index == len(container) {
				return append(container, value), nil
			}
			container[seg.index] = value
			return container, nil
		}
		if seg.index >= len(container) {
			return nil, &pathError{path: path, why: "index past the end"}
		}
		next, err := setPath(container[seg.index], path[1:], value)
		if err != nil {
			return nil, err
		}
		container[seg.index] = next
		return container, nil
	default:
		return nil, &pathError{path: path, why: "path runs through a scalar"}
	}
}

// deletePath is ["d"]. A missing object key is fine (JS delete succeeds); a
// missing array index is not, because splice there would silently do nothing.
func deletePath(node any, path []opSegment) (any, error) {
	if len(path) == 0 {
		return nil, &pathError{path: path, why: "op targets the root"}
	}
	seg := path[0]
	switch container := node.(type) {
	case map[string]any:
		if seg.isIdx {
			return nil, &pathError{path: path, why: "index into an object"}
		}
		if len(path) == 1 {
			delete(container, seg.key)
			return container, nil
		}
		child, ok := container[seg.key]
		if !ok {
			return nil, &pathError{path: path}
		}
		next, err := deletePath(child, path[1:])
		if err != nil {
			return nil, err
		}
		container[seg.key] = next
		return container, nil
	case []any:
		if !seg.isIdx {
			return nil, &pathError{path: path, why: "key into an array"}
		}
		if seg.index >= len(container) {
			return nil, &pathError{path: path, why: "index past the end"}
		}
		if len(path) == 1 {
			return append(container[:seg.index], container[seg.index+1:]...), nil
		}
		next, err := deletePath(container[seg.index], path[1:])
		if err != nil {
			return nil, err
		}
		container[seg.index] = next
		return container, nil
	default:
		return nil, &pathError{path: path, why: "path runs through a scalar"}
	}
}

// updateString backs ["a"] and ["t"]: the target must be a string, and the
// replacement is written back through the same parent walk as setPath.
func updateString(node any, path []opSegment, edit func(string) string) (any, error) {
	if len(path) == 0 {
		return nil, &pathError{path: path, why: "op targets the root"}
	}
	seg := path[0]
	switch container := node.(type) {
	case map[string]any:
		if seg.isIdx {
			return nil, &pathError{path: path, why: "index into an object"}
		}
		child, ok := container[seg.key]
		if !ok {
			return nil, &pathError{path: path}
		}
		if len(path) == 1 {
			text, ok := child.(string)
			if !ok {
				return nil, &pathError{path: path, why: "target is not a string"}
			}
			container[seg.key] = edit(text)
			return container, nil
		}
		next, err := updateString(child, path[1:], edit)
		if err != nil {
			return nil, err
		}
		container[seg.key] = next
		return container, nil
	case []any:
		if !seg.isIdx {
			return nil, &pathError{path: path, why: "key into an array"}
		}
		if seg.index >= len(container) {
			return nil, &pathError{path: path, why: "index past the end"}
		}
		if len(path) == 1 {
			text, ok := container[seg.index].(string)
			if !ok {
				return nil, &pathError{path: path, why: "target is not a string"}
			}
			container[seg.index] = edit(text)
			return container, nil
		}
		next, err := updateString(container[seg.index], path[1:], edit)
		if err != nil {
			return nil, err
		}
		container[seg.index] = next
		return container, nil
	default:
		return nil, &pathError{path: path, why: "path runs through a scalar"}
	}
}

// splicePath is ["p"]: JS Array.prototype.splice with non-negative start and
// deleteCount. Both clamp to the array length, exactly like the browser.
func splicePath(root any, path []opSegment, start, remove int, items []any) (any, error) {
	target, err := resolveNode(root, path)
	if err != nil {
		return nil, err
	}
	array, ok := target.([]any)
	if !ok {
		return nil, &pathError{path: path, why: "p target is not an array"}
	}
	if start > len(array) {
		start = len(array)
	}
	if remove > len(array)-start {
		remove = len(array) - start
	}
	next := make([]any, 0, len(array)-remove+len(items))
	next = append(next, array[:start]...)
	next = append(next, items...)
	next = append(next, array[start+remove:]...)
	return replaceNode(root, path, next)
}

// permutePath is ["m"]: new[i] = old[perm[i]]. Unlike JS, a permutation that
// is not a bijection is an error rather than a row of undefineds.
func permutePath(root any, path []opSegment, perm []int) (any, error) {
	target, err := resolveNode(root, path)
	if err != nil {
		return nil, err
	}
	array, ok := target.([]any)
	if !ok {
		return nil, &pathError{path: path, why: "m target is not an array"}
	}
	if len(perm) != len(array) {
		return nil, &pathError{path: path, why: fmt.Sprintf("permutation length %d does not match array %d", len(perm), len(array))}
	}
	seen := make([]bool, len(array))
	next := make([]any, len(array))
	for i, from := range perm {
		if from < 0 || from >= len(array) || seen[from] {
			return nil, &pathError{path: path, why: "permutation is not a bijection"}
		}
		seen[from] = true
		next[i] = array[from]
	}
	return replaceNode(root, path, next)
}

// resolveNode walks existing containers only; a missing key or a scalar on the
// way is an error, never a zero value.
func resolveNode(root any, path []opSegment) (any, error) {
	node := root
	for i, seg := range path {
		switch container := node.(type) {
		case map[string]any:
			if seg.isIdx {
				return nil, &pathError{path: path[:i+1], why: "index into an object"}
			}
			child, ok := container[seg.key]
			if !ok {
				return nil, &pathError{path: path[:i+1]}
			}
			node = child
		case []any:
			if !seg.isIdx {
				return nil, &pathError{path: path[:i+1], why: "key into an array"}
			}
			if seg.index >= len(container) {
				return nil, &pathError{path: path[:i+1], why: "index past the end"}
			}
			node = container[seg.index]
		default:
			return nil, &pathError{path: path[:i+1], why: "path runs through a scalar"}
		}
	}
	return node, nil
}

func replaceNode(root any, path []opSegment, value any) (any, error) {
	if len(path) == 0 {
		return value, nil
	}
	return setPath(root, path, value)
}

// project turns the tree back into the frame value app.go consumes. Only
// entries and docs["pi.live"] are taken: filling pi.inbox here would make
// queued messages appear in the composer, a visible behaviour change.
//
// Marshalling the two subtrees and decoding them into protocol.FrameValue keeps
// one schema (protocol.go) instead of a hand-written converter that drifts.
func project(root any) (protocol.FrameValue, error) {
	view, ok := root.(map[string]any)
	if !ok {
		return protocol.FrameValue{}, fmt.Errorf("view is not an object")
	}
	entries, ok := view["entries"].([]any)
	if !ok {
		return protocol.FrameValue{}, fmt.Errorf("view.entries is not an array")
	}
	// A view without pi.live is still drawable (the old decoder omitted it);
	// one whose pi.live is not an object is not.
	docs := map[string]any{}
	if rawDocs, ok := view["docs"]; ok {
		decoded, ok := rawDocs.(map[string]any)
		if !ok {
			return protocol.FrameValue{}, fmt.Errorf("view.docs is not an object")
		}
		docs = decoded
	}
	live, hasLive := docs["pi.live"]
	if hasLive {
		if _, ok := live.(map[string]any); !ok {
			return protocol.FrameValue{}, fmt.Errorf("view.docs[pi.live] is not an object")
		}
	}
	payload := map[string]any{"entries": entries, "docs": map[string]any{}}
	if hasLive {
		payload["docs"] = map[string]any{"pi.live": live}
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return protocol.FrameValue{}, err
	}
	var value protocol.FrameValue
	if err := decodeInto(raw, &value); err != nil {
		return protocol.FrameValue{}, err
	}
	return value, nil
}
