package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// These tests pass. They pin down four behaviours of the tutorial code that
// are fine for a tutorial and unsafe for anything else. Each one is the
// reason for a specific change in internal/tools and internal/workspace,
// where the matching eval asserts the opposite.

func input(t *testing.T, v any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// The model chooses the path, and nothing limits it to the project.
// Fixed by: workspace.Clean and os.Root. Eval: read-parent-traversal.
func TestBaseline_ReadsOutsideTheWorkingDirectory(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "outside.txt"), []byte("not yours"), 0o600); err != nil {
		t.Fatal(err)
	}
	project := filepath.Join(root, "project")
	if err := os.Mkdir(project, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(project)

	got, err := ReadFile(input(t, ReadFileInput{Path: "../outside.txt"}))
	if err != nil || got != "not yours" {
		t.Fatalf("expected the tutorial code to read outside the project, got %q, %v", got, err)
	}
}

// strings.Replace with an empty old string inserts the new string between
// every character. Fixed by: refusing an empty old_str on an existing file.
// Eval: edit-empty-old-str-on-existing-file.
func TestBaseline_EmptyOldStrCorruptsAnExistingFile(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile("notes.txt", []byte("abc"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := EditFile(input(t, EditFileInput{Path: "notes.txt", OldStr: "", NewStr: "X"})); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile("notes.txt")
	if string(got) != "XaXbXcX" {
		t.Fatalf("expected the file to be corrupted to XaXbXcX, got %q", got)
	}
}

// The schema tells the model old_str "must only have one match", but the
// code replaces all of them. Fixed by: counting matches and refusing more
// than one. Eval: edit-ambiguous-match.
func TestBaseline_ReplacesEveryMatch(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile("app.js", []byte("total = total + 1"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := EditFile(input(t, EditFileInput{Path: "app.js", OldStr: "total", NewStr: "sum"})); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile("app.js")
	if strings.Count(string(got), "sum") != 2 {
		t.Fatalf("expected both occurrences to be replaced, got %q", got)
	}
}

// Malformed input from the model panics, which in a server would end the
// process for every user. Fixed by: returning the error to the model.
// Eval: input-wrong-type.
func TestBaseline_ListFilesPanicsOnMalformedInput(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected the tutorial code to panic")
		}
	}()
	_, _ = ListFiles(json.RawMessage(`{"path": 42}`))
}
