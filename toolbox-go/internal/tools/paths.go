package tools

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"pilot/toolbox/internal/unicodenorm"
	"strings"
	"syscall"
)

// normalizeToolPath mirrors pi-durable's path-utils: unicode spaces become plain spaces and a
// leading "@" (as pasted from chat clients) is dropped.
func normalizeToolPath(path string) string {
	var b strings.Builder
	b.Grow(len(path))
	for _, r := range path {
		if isUnicodeSpace(r) {
			b.WriteRune(' ')
			continue
		}
		b.WriteRune(r)
	}
	return strings.TrimPrefix(b.String(), "@")
}

func isUnicodeSpace(r rune) bool {
	switch r {
	case '\u00a0', '\u202f', '\u205f', '\u3000':
		return true
	}
	return r >= '\u2000' && r <= '\u200a'
}

// resolvePath mirrors NodeExecutionEnv.absolutePath: "~", "~/", "file://" then absolute-or-relative
// against the working directory.
// resolveReadPath is pi-durable's resolveReadToolPath: before giving up on a path, `read` tries the
// spellings macOS and copy-pasted text produce for the same file — as given, with a narrow no-break
// space before AM/PM, NFD, the curly apostrophe, and NFD with the curly apostrophe.
func resolveReadPath(absolutePath string) string {
	curly := strings.ReplaceAll(absolutePath, "'", "\u2019")
	variants := []string{
		absolutePath,
		collapseAMPMSpaces(absolutePath),
		unicodenorm.NFD(absolutePath),
		curly,
		unicodenorm.NFD(curly),
	}
	seen := map[string]bool{}
	for _, variant := range variants {
		if seen[variant] {
			continue
		}
		seen[variant] = true
		if exists(variant) {
			return variant
		}
	}
	return absolutePath
}

// collapseAMPMSpaces follows the reference regex `/ (AM|PM)\./gi`, replacing the plain space with
// a narrow no-break space, which is what a Finder-renamed "3 PM." filename contains.
func collapseAMPMSpaces(path string) string {
	for _, marker := range []string{" AM.", " PM.", " am.", " pm.", " Am.", " Pm.", " aM.", " pM."} {
		path = strings.ReplaceAll(path, marker, "\u202F"+marker[1:])
	}
	return path
}

func resolvePath(cwd, path string) string {
	normalized := normalizeToolPath(path)
	switch {
	case normalized == "~":
		normalized = homeDir()
	case strings.HasPrefix(normalized, "~/"):
		normalized = filepath.Join(homeDir(), normalized[2:])
	case strings.HasPrefix(normalized, "file://"):
		if u, parseErr := url.Parse(normalized); parseErr == nil {
			normalized = u.Path
		}
	}
	if filepath.IsAbs(normalized) {
		return filepath.Clean(normalized)
	}
	return filepath.Join(cwd, normalized)
}

func homeDir() string {
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return home
	}
	return os.Getenv("HOME")
}

// fileErrorCode maps an os error to pi-durable's FileError codes so callers can keep the same
// "Error code: <code>." wording.
func fileErrorCode(err error) string {
	var errno syscall.Errno
	if errors.As(err, &errno) {
		switch errno {
		case syscall.ENOENT:
			return "not_found"
		case syscall.EACCES, syscall.EPERM:
			return "permission_denied"
		case syscall.ENOTDIR:
			return "not_directory"
		case syscall.EISDIR:
			return "is_directory"
		case syscall.EINVAL:
			return "invalid"
		}
	}
	if os.IsNotExist(err) {
		return "not_found"
	}
	if os.IsPermission(err) {
		return "permission_denied"
	}
	return "unknown"
}

// nodeStyleFileError reproduces the error Node's fs raises, because `read` and `write` let it reach
// the model untouched: "<CODE>: <strerror>, open '<path>'".
// nodeReadError is the same for a read: Node's error carries no path for the read syscall, so a
// directory reads as "EISDIR: illegal operation on a directory, read".
func nodeReadError(err error, absolutePath string) error {
	if err != nil && errors.Is(err, syscall.EISDIR) {
		return fmt.Errorf("EISDIR: illegal operation on a directory, read")
	}
	return nodeStyleFileError(err, absolutePath)
}

// nodeWriteError covers the two places a write can fail: realpath of the target, which Node's
// mkdir reports for a parent that is a file, and the open itself.
func nodeWriteError(err error, absolutePath string) error {
	if err != nil && errors.Is(err, syscall.ENOTDIR) {
		return fmt.Errorf("ENOTDIR: %s, realpath '%s'", nodeStrerror(syscall.ENOTDIR), absolutePath)
	}
	return nodeStyleFileError(err, absolutePath)
}

func nodeStyleFileError(err error, absolutePath string) error {
	if err == nil {
		return nil
	}
	var errno syscall.Errno
	if !errors.As(err, &errno) {
		return err
	}
	return fmt.Errorf("%s: %s, open '%s'", errnoName(errno), nodeStrerror(errno), absolutePath)
}

// errnoName is the symbolic name Node prefixes its fs errors with.
func errnoName(errno syscall.Errno) string {
	switch errno {
	case syscall.ENOENT:
		return "ENOENT"
	case syscall.EACCES:
		return "EACCES"
	case syscall.EPERM:
		return "EPERM"
	case syscall.EISDIR:
		return "EISDIR"
	case syscall.ENOTDIR:
		return "ENOTDIR"
	case syscall.ENAMETOOLONG:
		return "ENAMETOOLONG"
	case syscall.EROFS:
		return "EROFS"
	case syscall.ENOSPC:
		return "ENOSPC"
	case syscall.EMFILE:
		return "EMFILE"
	case syscall.ELOOP:
		return "ELOOP"
	case syscall.EINVAL:
		return "EINVAL"
	default:
		return strings.ToUpper(errno.Error())
	}
}

// nodeStrerror is libuv's wording for the common errnos; it differs from strerror in a few places
// (EISDIR notably), and the text is model-visible.
func nodeStrerror(errno syscall.Errno) string {
	switch errno {
	case syscall.ENOENT:
		return "no such file or directory"
	case syscall.EACCES:
		return "permission denied"
	case syscall.EPERM:
		return "operation not permitted"
	case syscall.EISDIR:
		return "illegal operation on a directory"
	case syscall.ENOTDIR:
		return "not a directory"
	case syscall.ENAMETOOLONG:
		return "name too long"
	case syscall.EROFS:
		return "read-only file system"
	case syscall.ENOSPC:
		return "no space left on device"
	case syscall.EMFILE:
		return "too many open files"
	case syscall.ELOOP:
		return "too many symbolic links encountered"
	case syscall.EINVAL:
		return "invalid argument"
	default:
		return errno.Error()
	}
}
