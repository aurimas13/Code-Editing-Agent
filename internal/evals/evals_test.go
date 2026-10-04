package evals

import (
	"context"
	"strings"
	"testing"
	"time"

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

// The live suite costs money, so it runs on demand; the free suites run on
// every commit and rewrite the report. That rewrite must keep the last live
// result, say where it came from, and drop it the moment a case changes.
func TestKeepLive(t *testing.T) {
	live := Suite{Name: "Live model", Kind: "live", Cases: []Case{{ID: "a", Prompt: "do a"}, {ID: "b", Prompt: "do b"}}}
	ranAt := time.Date(2026, 10, 4, 21, 36, 0, 0, time.UTC)

	// What a report written by an earlier version looks like: no per-suite
	// date or hash, only the report's own.
	old := Report{GeneratedAt: ranAt, GitSHA: "76f03e5", Model: "claude-haiku-4-5", Suites: []SuiteResult{{
		Name: "Live model", Kind: "live", Ran: true, Passed: 2, Total: 2,
		Cases: []CaseResult{{ID: "a", Prompt: "do a", Ran: true, Passed: true}, {ID: "b", Prompt: "do b", Ran: true, Passed: true}},
	}}}

	fresh := Run(context.Background(), []Suite{live}, Options{})
	if fresh.Suites[0].Ran || fresh.Suites[0].CasesHash == "" {
		t.Fatalf("a run without a client: %+v", fresh.Suites[0])
	}
	if kept := fresh.KeepLive(old); len(kept) != 1 {
		t.Fatalf("kept %v, want the live suite", kept)
	}
	got := fresh.Suites[0]
	if !got.Ran || got.Passed != 2 || got.Model != "claude-haiku-4-5" || got.GitSHA != "76f03e5" || got.RanAt == nil || !got.RanAt.Equal(ranAt) {
		t.Errorf("kept result lost where it came from: %+v", got)
	}

	// Kept again from the new-style report, by hash.
	again := Run(context.Background(), []Suite{live}, Options{})
	if kept := again.KeepLive(fresh); len(kept) != 1 || !again.Suites[0].RanAt.Equal(ranAt) {
		t.Errorf("second carry-over: kept %v, %+v", kept, again.Suites[0])
	}

	// Reword one case and the old result no longer speaks for the suite.
	live.Cases[1].Prompt = "do b differently"
	changed := Run(context.Background(), []Suite{live}, Options{})
	if kept := changed.KeepLive(fresh); len(kept) != 0 || changed.Suites[0].Ran {
		t.Errorf("a result for different cases was kept: %v", kept)
	}
}
