package workspace

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClean(t *testing.T) {
	ok := map[string]string{
		"main.go":         "main.go",
		"./main.go":       "main.go",
		"src/../main.go":  "main.go",
		"src//lib/a.js":   "src/lib/a.js",
		".":               ".",
		`src\lib\a.js`:    "src/lib/a.js",
		"a/b/../../c.txt": "c.txt",
		"dir.with.dots/x": "dir.with.dots/x",
		"..hidden/file":   "..hidden/file", // a name that merely starts with dots
		"file..txt":       "file..txt",
	}
	for in, want := range ok {
		got, err := Clean(in)
		if err != nil || got != want {
			t.Errorf("Clean(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	bad := []string{
		"", "..", "../x", "a/../../x", "/etc/passwd", "~/x", "~", `C:\x`, "c:/x",
		`..\..\x`, "a\x00b", "a\nb", strings.Repeat("a", MaxPathLen+1),
	}
	for _, in := range bad {
		if got, err := Clean(in); err == nil {
			t.Errorf("Clean(%q) = %q, want error", in, got)
		}
	}
}

func TestSensitive(t *testing.T) {
	yes := []string{".env", ".env.local", "config/.env.production", ".git/config", ".ssh/id_rsa",
		"deploy/key.pem", "certs/server.KEY", "id_ed25519", ".aws/credentials", ".npmrc"}
	no := []string{"main.go", ".env.example", "env.go", "docs/keys.md", ".github/workflows/ci.yml", "gitignore"}
	for _, p := range yes {
		if !Sensitive(p) {
			t.Errorf("Sensitive(%q) = false, want true", p)
		}
	}
	for _, p := range no {
		if Sensitive(p) {
			t.Errorf("Sensitive(%q) = true, want false", p)
		}
	}
}

func TestMemFSQuotas(t *testing.T) {
	fs, err := NewMemFS(Limits{MaxFiles: 2, MaxFileBytes: 10, MaxTotalBytes: 15}, map[string]string{"a.txt": "12345"})
	if err != nil {
		t.Fatal(err)
	}
	if err := fs.WriteFile("b.txt", []byte("12345678901")); !errors.Is(err, ErrQuota) {
		t.Errorf("oversized file: got %v, want ErrQuota", err)
	}
	if err := fs.WriteFile("b.txt", []byte("1234567890")); err != nil {
		t.Errorf("file at the limit: %v", err)
	}
	if err := fs.WriteFile("c.txt", []byte("1")); !errors.Is(err, ErrQuota) {
		t.Errorf("third file: got %v, want ErrQuota", err)
	}
	// Growing a.txt would take the total past 15 bytes.
	if err := fs.WriteFile("a.txt", []byte("123456")); !errors.Is(err, ErrQuota) {
		t.Errorf("total bytes: got %v, want ErrQuota", err)
	}
	// Shrinking an existing file is always allowed.
	if err := fs.WriteFile("a.txt", []byte("1")); err != nil {
		t.Errorf("shrinking: %v", err)
	}
}

func TestMemFSIsolation(t *testing.T) {
	seed := map[string]string{"a.txt": "one"}
	a, _ := NewMemFS(DemoLimits, seed)
	b, _ := NewMemFS(DemoLimits, seed)
	if err := a.WriteFile("a.txt", []byte("changed")); err != nil {
		t.Fatal(err)
	}
	got, _ := b.ReadFile("a.txt")
	if string(got) != "one" {
		t.Errorf("a write in one workspace was visible in another: %q", got)
	}
	// Returned slices are copies; mutating them must not change the file.
	data, _ := a.ReadFile("a.txt")
	data[0] = 'X'
	again, _ := a.ReadFile("a.txt")
	if string(again) != "changed" {
		t.Errorf("ReadFile exposed internal storage: %q", again)
	}
}

func TestMemFSList(t *testing.T) {
	fs, _ := NewMemFS(DemoLimits, map[string]string{"main.go": "", "src/a.js": "", "src/lib/b.js": ""})
	entries, err := fs.List("")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range entries {
		name := e.Path
		if e.Dir {
			name += "/"
		}
		got = append(got, name)
	}
	want := "main.go src/ src/a.js src/lib/ src/lib/b.js"
	if strings.Join(got, " ") != want {
		t.Errorf("List = %v, want %s", got, want)
	}
	sub, err := fs.List("src")
	if err != nil || len(sub) != 3 || sub[0].Path != "a.js" {
		t.Errorf("List(src) = %v, %v", sub, err)
	}
	if _, err := fs.List("nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("List(nope) = %v, want ErrNotFound", err)
	}
}

func TestDiskFSConfinement(t *testing.T) {
	base := t.TempDir()
	if err := os.WriteFile(filepath.Join(base, "outside.txt"), []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	project := filepath.Join(base, "project")
	must(t, os.MkdirAll(filepath.Join(project, ".git"), 0o755))
	must(t, os.MkdirAll(filepath.Join(project, "node_modules", "x"), 0o755))
	must(t, os.WriteFile(filepath.Join(project, "main.go"), []byte("package main\n"), 0o644))
	must(t, os.WriteFile(filepath.Join(project, ".env"), []byte("KEY=1"), 0o600))
	must(t, os.WriteFile(filepath.Join(project, ".git", "config"), []byte("[core]"), 0o644))
	// A symlink inside the project that points outside it. String checks on
	// the path cannot see this; os.Root refuses to follow it.
	must(t, os.Symlink(filepath.Join(base, "outside.txt"), filepath.Join(project, "link.txt")))
	must(t, os.Symlink(base, filepath.Join(project, "up")))

	fs, err := OpenDiskFS(project, LocalLimits)
	must(t, err)
	defer fs.Close()

	if got, err := fs.ReadFile("main.go"); err != nil || string(got) != "package main\n" {
		t.Errorf("ReadFile(main.go) = %q, %v", got, err)
	}
	for _, p := range []string{"../outside.txt", "link.txt", "up/outside.txt"} {
		if got, err := fs.ReadFile(p); err == nil {
			t.Errorf("ReadFile(%q) escaped the workspace and returned %q", p, got)
		}
	}
	if err := fs.WriteFile("up/planted.txt", []byte("x")); err == nil {
		t.Error("WriteFile through an escaping symlink succeeded")
	}
	if _, err := os.Stat(filepath.Join(base, "planted.txt")); err == nil {
		t.Error("a file was written outside the workspace")
	}
	for _, p := range []string{".env", ".git/config"} {
		if _, err := fs.ReadFile(p); !errors.Is(err, ErrSensitive) {
			t.Errorf("ReadFile(%q) = %v, want ErrSensitive", p, err)
		}
	}
	if err := fs.WriteFile(".env", []byte("x")); !errors.Is(err, ErrSensitive) {
		t.Errorf("WriteFile(.env) = %v, want ErrSensitive", err)
	}

	must(t, fs.WriteFile("src/lib/new.go", []byte("package lib\n")))
	entries, err := fs.List("")
	must(t, err)
	var names []string
	for _, e := range entries {
		names = append(names, e.Path)
	}
	listing := strings.Join(names, " ")
	for _, hidden := range []string{".git", ".env", "node_modules"} {
		if strings.Contains(listing, hidden) {
			t.Errorf("List exposed %s: %v", hidden, names)
		}
	}
	if !strings.Contains(listing, "src/lib/new.go") {
		t.Errorf("List is missing the new file: %v", names)
	}
	// Error messages must not reveal where the workspace lives on the host.
	_, err = fs.ReadFile("missing.txt")
	if err == nil || strings.Contains(err.Error(), base) {
		t.Errorf("error leaks the host path: %v", err)
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
