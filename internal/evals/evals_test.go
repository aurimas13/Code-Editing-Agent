package evals

import (
	"context"
	"testing"
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
