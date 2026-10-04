// Package workspace is the only way the agent's tools touch files.
//
// The tutorial version called os.ReadFile and os.WriteFile with whatever path
// the model produced, so the model could read ~/.ssh/id_rsa or overwrite
// /etc/hosts. Here every path is validated once, in one place, and the
// filesystem behind it is confined: a per-visitor in-memory tree on the web
// (MemFS) or a directory opened with os.Root on a developer machine (DiskFS).
package workspace

import (
	"errors"
	"fmt"
	"path"
	"strings"
	"unicode"
)

// MaxPathLen bounds the length of any path a tool may address.
const MaxPathLen = 240

var (
	// ErrEscape is returned for paths that point outside the workspace.
	ErrEscape = errors.New("path escapes the workspace")
	// ErrSensitive is returned for paths that match the deny list.
	ErrSensitive = errors.New("path is on the sensitive-file deny list")
	// ErrNotFound is returned when a file does not exist.
	ErrNotFound = errors.New("file does not exist")
	// ErrIsDir is returned when a file operation targets a directory.
	ErrIsDir = errors.New("path is a directory")
	// ErrQuota is returned when a write would exceed a workspace limit.
	ErrQuota = errors.New("workspace quota exceeded")
)

// Clean validates a model-supplied path and returns it in canonical form:
// relative, slash-separated, with no "." or ".." segments. The workspace root
// itself is ".".
//
// It rejects rather than repairs. A path like "../../etc/passwd" is an error,
// not "etc/passwd", so the model is told what it did wrong and the attempt
// shows up in the trace.
func Clean(p string) (string, error) {
	if p == "" {
		return "", errors.New("path is required")
	}
	if len(p) > MaxPathLen {
		return "", fmt.Errorf("path is longer than %d bytes", MaxPathLen)
	}
	for _, r := range p {
		if r == 0 || unicode.IsControl(r) {
			return "", errors.New("path contains control characters")
		}
	}
	p = strings.ReplaceAll(p, `\`, "/")
	if strings.HasPrefix(p, "/") || strings.HasPrefix(p, "~") || hasDriveLetter(p) {
		return "", fmt.Errorf("%w: use a path relative to the workspace root", ErrEscape)
	}
	// Check segments before path.Clean collapses them, so "a/../../b" cannot
	// hide a traversal behind a harmless-looking prefix.
	depth := 0
	for _, seg := range strings.Split(p, "/") {
		switch seg {
		case "", ".":
		case "..":
			depth--
			if depth < 0 {
				return "", ErrEscape
			}
		default:
			depth++
		}
	}
	return path.Clean(p), nil
}

func hasDriveLetter(p string) bool {
	return len(p) >= 2 && p[1] == ':' && unicode.IsLetter(rune(p[0]))
}

// Directories that are never listed, read, or written on a real disk.
var sensitiveDirs = map[string]bool{
	".git": true, ".ssh": true, ".aws": true, ".gnupg": true, ".kube": true, ".docker": true,
}

// File names that are never read or written on a real disk.
var sensitiveNames = map[string]bool{
	".env": true, ".netrc": true, ".npmrc": true, ".pypirc": true,
	"id_rsa": true, "id_dsa": true, "id_ecdsa": true, "id_ed25519": true,
	"credentials": true, "credentials.json": true, "secrets.json": true,
}

var sensitiveSuffixes = []string{".pem", ".key", ".p12", ".pfx", ".keystore"}

// Sensitive reports whether a cleaned path matches the deny list. It is a
// second line of defence for DiskFS: confinement keeps the agent inside the
// project directory, and the deny list keeps it away from the secrets that
// commonly live inside one.
func Sensitive(clean string) bool {
	segs := strings.Split(clean, "/")
	for _, seg := range segs[:len(segs)-1] {
		if sensitiveDirs[seg] {
			return true
		}
	}
	name := strings.ToLower(segs[len(segs)-1])
	if sensitiveDirs[name] || sensitiveNames[name] {
		return true
	}
	// .env.local, .env.production … but not the conventional template files.
	if strings.HasPrefix(name, ".env.") && name != ".env.example" && name != ".env.sample" {
		return true
	}
	for _, suf := range sensitiveSuffixes {
		if strings.HasSuffix(name, suf) {
			return true
		}
	}
	return false
}
