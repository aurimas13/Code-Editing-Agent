package workspace

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

// MemFS is an in-memory workspace. Each visitor to the public demo gets one,
// so nothing a visitor's agent does can reach the host disk or another
// visitor's files. It holds text only and enforces quotas on every write.
type MemFS struct {
	mu     sync.RWMutex
	files  map[string][]byte
	limits Limits
}

// NewMemFS returns a workspace seeded with files. Seed paths bypass quotas
// but not path validation.
func NewMemFS(limits Limits, seed map[string]string) (*MemFS, error) {
	m := &MemFS{files: make(map[string][]byte, len(seed)), limits: limits}
	for name, content := range seed {
		clean, err := Clean(name)
		if err != nil {
			return nil, fmt.Errorf("seed %q: %w", name, err)
		}
		m.files[clean] = []byte(content)
	}
	return m, nil
}

func (m *MemFS) ReadFile(name string) ([]byte, error) {
	clean, err := Clean(name)
	if err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	data, ok := m.files[clean]
	if !ok {
		if m.isDirLocked(clean) {
			return nil, fmt.Errorf("%s: %w", clean, ErrIsDir)
		}
		return nil, fmt.Errorf("%s: %w", clean, ErrNotFound)
	}
	return append([]byte(nil), data...), nil
}

func (m *MemFS) WriteFile(name string, data []byte) error {
	clean, err := Clean(name)
	if err != nil {
		return err
	}
	if clean == "." {
		return fmt.Errorf("%s: %w", clean, ErrIsDir)
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.isDirLocked(clean) {
		return fmt.Errorf("%s: %w", clean, ErrIsDir)
	}
	// "a/b.txt" is not writable while "a" is itself a file.
	for dir := parent(clean); dir != "."; dir = parent(dir) {
		if _, isFile := m.files[dir]; isFile {
			return fmt.Errorf("%s: parent %s is a file, not a directory", clean, dir)
		}
	}
	if m.limits.MaxFileBytes > 0 && int64(len(data)) > m.limits.MaxFileBytes {
		return fmt.Errorf("%w: file would be %d bytes, limit is %d", ErrQuota, len(data), m.limits.MaxFileBytes)
	}
	old, existed := m.files[clean]
	if !existed && m.limits.MaxFiles > 0 && len(m.files) >= m.limits.MaxFiles {
		return fmt.Errorf("%w: workspace already holds %d files", ErrQuota, len(m.files))
	}
	if m.limits.MaxTotalBytes > 0 {
		total := int64(len(data)) - int64(len(old))
		for _, f := range m.files {
			total += int64(len(f))
		}
		if total > m.limits.MaxTotalBytes {
			return fmt.Errorf("%w: workspace would be %d bytes, limit is %d", ErrQuota, total, m.limits.MaxTotalBytes)
		}
	}
	m.files[clean] = append([]byte(nil), data...)
	return nil
}

func (m *MemFS) Exists(name string) (bool, error) {
	clean, err := Clean(name)
	if err != nil {
		return false, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.files[clean]
	return ok, nil
}

func (m *MemFS) List(dir string) ([]Entry, error) {
	if dir == "" {
		dir = "."
	}
	clean, err := Clean(dir)
	if err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if _, isFile := m.files[clean]; isFile {
		return nil, fmt.Errorf("%s is a file, not a directory", clean)
	}
	prefix := ""
	if clean != "." {
		prefix = clean + "/"
	}
	seenDirs := map[string]bool{}
	var out []Entry
	for name, data := range m.files {
		if !strings.HasPrefix(name, prefix) {
			continue
		}
		rel := strings.TrimPrefix(name, prefix)
		out = append(out, Entry{Path: rel, Size: int64(len(data))})
		for d := parent(rel); d != "."; d = parent(d) {
			if !seenDirs[d] {
				seenDirs[d] = true
				out = append(out, Entry{Path: d, Dir: true})
			}
		}
	}
	if len(out) == 0 && clean != "." {
		return nil, fmt.Errorf("%s: %w", clean, ErrNotFound)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	if m.limits.MaxEntries > 0 && len(out) > m.limits.MaxEntries {
		out = out[:m.limits.MaxEntries]
	}
	return out, nil
}

// Snapshot returns a copy of every file, for persistence and for the UI.
func (m *MemFS) Snapshot() map[string]string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make(map[string]string, len(m.files))
	for name, data := range m.files {
		out[name] = string(data)
	}
	return out
}

func (m *MemFS) isDirLocked(clean string) bool {
	if clean == "." {
		return true
	}
	prefix := clean + "/"
	for name := range m.files {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

func parent(p string) string {
	i := strings.LastIndexByte(p, '/')
	if i < 0 {
		return "."
	}
	return p[:i]
}
