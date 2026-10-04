package workspace

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"
)

// DiskFS is a workspace backed by a real directory, used by the CLI.
//
// Confinement comes from os.Root (Go 1.24): every open is resolved relative
// to the root directory by the kernel, so "..", absolute paths, and symlinks
// that point outside the directory all fail, including when the link is
// swapped in between a check and the open. String checks on paths cannot give
// that guarantee, which is why Clean is treated as input validation here and
// os.Root as the boundary.
type DiskFS struct {
	root   *os.Root
	limits Limits
}

// OpenDiskFS opens dir as a confined workspace.
func OpenDiskFS(dir string, limits Limits) (*DiskFS, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, fmt.Errorf("open workspace: %w", err)
	}
	return &DiskFS{root: root, limits: limits}, nil
}

// Close releases the directory handle.
func (d *DiskFS) Close() error { return d.root.Close() }

func (d *DiskFS) resolve(name string) (string, error) {
	clean, err := Clean(name)
	if err != nil {
		return "", err
	}
	if Sensitive(clean) {
		return "", fmt.Errorf("%s: %w", clean, ErrSensitive)
	}
	return clean, nil
}

func (d *DiskFS) ReadFile(name string) ([]byte, error) {
	clean, err := d.resolve(name)
	if err != nil {
		return nil, err
	}
	f, err := d.root.Open(clean)
	if err != nil {
		return nil, mapErr(clean, err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, mapErr(clean, err)
	}
	if info.IsDir() {
		return nil, fmt.Errorf("%s: %w", clean, ErrIsDir)
	}
	if d.limits.MaxFileBytes > 0 && info.Size() > d.limits.MaxFileBytes {
		return nil, fmt.Errorf("%w: %s is %d bytes, limit is %d", ErrQuota, clean, info.Size(), d.limits.MaxFileBytes)
	}
	return io.ReadAll(f)
}

func (d *DiskFS) WriteFile(name string, data []byte) error {
	clean, err := d.resolve(name)
	if err != nil {
		return err
	}
	if clean == "." {
		return fmt.Errorf("%s: %w", clean, ErrIsDir)
	}
	if d.limits.MaxFileBytes > 0 && int64(len(data)) > d.limits.MaxFileBytes {
		return fmt.Errorf("%w: file would be %d bytes, limit is %d", ErrQuota, len(data), d.limits.MaxFileBytes)
	}
	if err := d.mkdirAll(parent(clean)); err != nil {
		return err
	}
	f, err := d.root.OpenFile(clean, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return mapErr(clean, err)
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return mapErr(clean, err)
	}
	return f.Close()
}

func (d *DiskFS) Exists(name string) (bool, error) {
	clean, err := d.resolve(name)
	if err != nil {
		return false, err
	}
	info, err := d.root.Stat(clean)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, mapErr(clean, err)
	}
	return !info.IsDir(), nil
}

func (d *DiskFS) List(dir string) ([]Entry, error) {
	if dir == "" {
		dir = "."
	}
	clean, err := d.resolve(dir)
	if err != nil {
		return nil, err
	}
	var out []Entry
	err = fs.WalkDir(d.root.FS(), clean, func(p string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if p == clean {
				return walkErr
			}
			return nil // unreadable subtree: skip it, keep listing
		}
		if p == clean {
			return nil
		}
		rel := strings.TrimPrefix(strings.TrimPrefix(p, clean), "/")
		if clean == "." {
			rel = p
		}
		if entry.IsDir() {
			if sensitiveDirs[entry.Name()] || entry.Name() == "node_modules" {
				return fs.SkipDir
			}
		} else if Sensitive(p) {
			return nil
		}
		e := Entry{Path: rel, Dir: entry.IsDir()}
		if info, err := entry.Info(); err == nil && !entry.IsDir() {
			e.Size = info.Size()
		}
		out = append(out, e)
		if d.limits.MaxEntries > 0 && len(out) >= d.limits.MaxEntries {
			return fs.SkipAll
		}
		return nil
	})
	if err != nil {
		return nil, mapErr(clean, err)
	}
	return out, nil
}

// mkdirAll creates dir and its parents inside the root. os.Root gained
// MkdirAll in Go 1.25; this keeps the module on 1.24.
func (d *DiskFS) mkdirAll(dir string) error {
	if dir == "." {
		return nil
	}
	built := ""
	for _, seg := range strings.Split(dir, "/") {
		if built == "" {
			built = seg
		} else {
			built += "/" + seg
		}
		err := d.root.Mkdir(built, 0o755)
		if err != nil && !errors.Is(err, fs.ErrExist) {
			return mapErr(built, err)
		}
	}
	return nil
}

// mapErr turns OS errors into messages the model can act on, without leaking
// absolute host paths into the conversation.
func mapErr(clean string, err error) error {
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return fmt.Errorf("%s: %w", clean, ErrNotFound)
	case errors.Is(err, fs.ErrPermission):
		return fmt.Errorf("%s: permission denied", clean)
	}
	var pathErr *fs.PathError
	if errors.As(err, &pathErr) {
		if strings.Contains(pathErr.Err.Error(), "escapes") {
			return fmt.Errorf("%s: %w", clean, ErrEscape)
		}
		return fmt.Errorf("%s: %v", clean, pathErr.Err)
	}
	return err
}
