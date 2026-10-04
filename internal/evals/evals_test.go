package evals

import (
	"context"
	"strings"
	"testing"

	"github.com/aurimas13/Code-Editing-Agent/internal/workspace"
)

// TestSuites runs every deterministic suite as part of `go test`, so a
// change that breaks a guarantee fails the build, not just a report.
func TestSuites(t *testing.T) {
	suites, err := LoadSuites("../../evals")
	if err != nil {
		t.Fatal(err)
	}
	if len(suites) < 3 {
		t.Fatalf("expected at least 3 suites, found %d", len(suites))
	}
	report := Run(context.Background(), suites, Options{})
	for _, s := range report.Suites {
		if s.Kind == "live" {
			if s.Ran {
				t.Errorf("%s: live suite ran without a client", s.Name)
			}
			continue
		}
		for _, c := range s.Cases {
			for _, f := range c.Failures {
				t.Errorf("%s / %s: %s", s.Name, c.ID, f)
			}
		}
	}
}

func TestExpand(t *testing.T) {
	if got := expand("a{{repeat:xy:3}}b"); got != "axyxyxyb" {
		t.Errorf("expand = %q", got)
	}
}

// A task can have several correct edits. "Print only until 15" is done as
// well by run(15) as by changing the default, and the first live runs failed
// a correct answer because the check named one of them.
func TestFileContainsAny(t *testing.T) {
	expect := Expect{Files: map[string]FileExpect{"f.js": {ContainsAny: []string{"limit = 15", "run(15)"}}}}
	for content, ok := range map[string]bool{
		"function run(limit = 100) {}\nrun(15);\n": true,
		"function run(limit = 15) {}\nrun();\n":    true,
		"function run(limit = 100) {}\nrun();\n":   false,
	} {
		fs, err := workspace.NewMemFS(workspace.DemoLimits, map[string]string{"f.js": content})
		if err != nil {
			t.Fatal(err)
		}
		var cr CaseResult
		checkFiles(expect, nil, fs, &cr)
		if passed := len(cr.Failures) == 0; passed != ok {
			t.Errorf("%q: passed = %v, want %v (%s)", content, passed, ok, strings.Join(cr.Failures, "; "))
		}
	}
}
