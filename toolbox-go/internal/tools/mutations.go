package tools

import (
	"path/filepath"
	"sync"
)

// fileMutations serializes `write` and `edit` on one file inside this process, so two calls that
// arrive at once cannot read-modify-write over each other. It is not a lock against `bash` or
// another process, which matches the reference implementation.
type fileMutations struct {
	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

var mutations = &fileMutations{locks: map[string]*sync.Mutex{}}

// withFileMutation runs fn while holding the lock of the file's canonical path.
func withFileMutation(absolutePath string, fn func() error) error {
	lock := mutations.lockFor(mutationKey(absolutePath))
	lock.Lock()
	defer lock.Unlock()
	return fn()
}

func (m *fileMutations) lockFor(key string) *sync.Mutex {
	m.mu.Lock()
	defer m.mu.Unlock()
	lock, ok := m.locks[key]
	if !ok {
		lock = &sync.Mutex{}
		m.locks[key] = lock
	}
	return lock
}

// mutationKey is the canonical path; a file that does not exist yet uses its canonical parent and
// its name, so a `write` that creates it and a later `edit` share one key even under a symlink.
func mutationKey(absolutePath string) string {
	if resolved, err := filepath.EvalSymlinks(absolutePath); err == nil {
		return resolved
	}
	parent := filepath.Dir(absolutePath)
	if resolvedParent, err := filepath.EvalSymlinks(parent); err == nil {
		if name, err := filepath.Rel(parent, absolutePath); err == nil {
			return filepath.Join(resolvedParent, name)
		}
	}
	return absolutePath
}
