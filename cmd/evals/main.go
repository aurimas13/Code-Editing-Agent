// Command evals runs the eval suites and writes a report.
//
//	go run ./cmd/evals              # tool and scripted suites (free, deterministic)
//	go run ./cmd/evals -live        # also the live suite (needs ANTHROPIC_API_KEY)
//
// It exits non-zero if any case that ran failed, so it can gate CI.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/anthropics/anthropic-sdk-go/option"

	"github.com/aurimas13/Code-Editing-Agent/internal/evals"
	"github.com/aurimas13/Code-Editing-Agent/internal/llm"
	"github.com/aurimas13/Code-Editing-Agent/internal/store"
)

func main() {
	dir := flag.String("dir", "evals", "directory of suite files")
	out := flag.String("out", "evals/results/latest.json", "where to write the report")
	web := flag.String("web", "web/src/generated/evals.json", "second copy for the website (empty to skip)")
	live := flag.Bool("live", false, "also run live suites against the real model")
	model := flag.String("model", "claude-haiku-4-5", "model for live suites")
	flag.Parse()

	suites, err := evals.LoadSuites(*dir)
	if err != nil {
		fatal(err)
	}
	opts := evals.Options{}
	if *live {
		if os.Getenv("ANTHROPIC_API_KEY") == "" {
			fatal(fmt.Errorf("-live needs ANTHROPIC_API_KEY"))
		}
		opts.Live = llm.NewAnthropic(option.WithMaxRetries(2))
		opts.LiveModel = *model
	}

	report := evals.Run(context.Background(), suites, opts)
	if sha, err := exec.Command("git", "rev-parse", "--short", "HEAD").Output(); err == nil {
		report.GitSHA = strings.TrimSpace(string(sha))
		for i := range report.Suites {
			if report.Suites[i].Kind == "live" && report.Suites[i].Ran {
				report.Suites[i].GitSHA = report.GitSHA
			}
		}
	}
	failed := report.Failed() // judged on what ran now, before any older result is kept

	for _, s := range report.Suites {
		if !s.Ran {
			fmt.Printf("\n%s: not run (%d cases; use -live)\n", s.Name, s.Total)
			continue
		}
		fmt.Printf("\n%s: %d/%d passed\n", s.Name, s.Passed, s.Total)
		for _, c := range s.Cases {
			mark := "PASS"
			if !c.Passed {
				mark = "FAIL"
			}
			fmt.Printf("  %s  %-40s %s\n", mark, c.ID, c.Category)
			for _, f := range c.Failures {
				fmt.Printf("        - %s\n", f)
			}
		}
	}

	// Keep a history of runs when a database is configured.
	if url, key := os.Getenv("SUPABASE_URL"), os.Getenv("SUPABASE_SECRET_KEY"); url != "" && key != "" {
		st := store.NewSupabase(url, key)
		for _, s := range report.Suites {
			if !s.Ran {
				continue
			}
			detail, _ := json.Marshal(s)
			run := store.EvalRun{Suite: s.Name, Model: report.Model, GitSHA: report.GitSHA,
				Passed: s.Passed, Failed: s.Failed, Total: s.Total, Report: detail}
			if err := st.SaveEvalRun(context.Background(), run); err != nil {
				fmt.Fprintln(os.Stderr, "warning: could not save eval run:", err)
			}
		}
	}

	// A run without -live must not erase the last live result from the report.
	if prev, err := os.ReadFile(*out); err == nil {
		var last evals.Report
		if json.Unmarshal(prev, &last) == nil {
			for _, name := range report.KeepLive(last) {
				fmt.Printf("\n%s: kept the result of the last live run\n", name)
			}
		}
	}

	raw, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		fatal(err)
	}
	raw = append(raw, '\n')
	for _, p := range []string{*out, *web} {
		if p == "" {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			fatal(err)
		}
		if err := os.WriteFile(p, raw, 0o644); err != nil {
			fatal(err)
		}
		fmt.Println("\nwrote", p)
	}

	if failed {
		os.Exit(1)
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "evals:", err)
	os.Exit(2)
}
