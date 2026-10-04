// Package evals measures the agent instead of assuming it works.
//
// There are three kinds of suite, from cheapest and most certain to most
// realistic:
//
//   - tool: call one tool directly with hostile or awkward input. These test
//     the sandbox and the edit rules. No model is involved, so a pass means
//     the property holds for every model and every prompt.
//   - scripted: run the whole loop against a fake model that replays a fixed
//     script. These test the loop's own behaviour (feeding errors back,
//     stopping, redacting) deterministically.
//   - live: run the whole loop against the real model and check outcomes.
//     These cost money and can vary between runs, so they are opt-in.
//
// Suites are JSON files under evals/ so cases can be added without touching
// Go code.
package evals

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go/option"

	"github.com/aurimas13/Code-Editing-Agent/internal/agent"
	"github.com/aurimas13/Code-Editing-Agent/internal/llm"
	"github.com/aurimas13/Code-Editing-Agent/internal/llm/fakellm"
	"github.com/aurimas13/Code-Editing-Agent/internal/tools"
	"github.com/aurimas13/Code-Editing-Agent/internal/workspace"
)

// Suite is a named set of cases of one kind.
type Suite struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Kind        string `json:"kind"` // "tool", "scripted", or "live"
	Cases       []Case `json:"cases"`
}

// Case is one eval. Which fields apply depends on the suite kind.
type Case struct {
	ID       string `json:"id"`
	Category string `json:"category"`
	// Why says what failure this case exists to catch.
	Why string `json:"why"`

	// Starting workspace. FillerFiles adds that many extra small files.
	Files       map[string]string `json:"files,omitempty"`
	FillerFiles int               `json:"filler_files,omitempty"`

	// tool suites
	Tool  string          `json:"tool,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`

	// scripted and live suites
	Prompt    string             `json:"prompt,omitempty"`
	Mode      string             `json:"mode,omitempty"` // "code" (default) or "research"
	Script    []fakellm.Response `json:"script,omitempty"`
	MaxRounds int                `json:"max_rounds,omitempty"`
	Approve   string             `json:"approve,omitempty"` // "deny" refuses every write

	Expect Expect `json:"expect"`
}

// Expect lists what must be true after a case runs. Unset fields are not
// checked.
type Expect struct {
	// tool suites
	OutputContains []string `json:"output_contains,omitempty"`
	ErrorContains  string   `json:"error_contains,omitempty"`

	// scripted and live suites
	ReplyContains    []string `json:"reply_contains,omitempty"`
	ReplyContainsAny []string `json:"reply_contains_any,omitempty"`
	ReplyNotContains []string `json:"reply_not_contains,omitempty"`
	ToolsCalled      []string `json:"tools_called,omitempty"` // in order, others may come between
	ToolsNotCalled   []string `json:"tools_not_called,omitempty"`
	ToolErrors       *int     `json:"tool_errors,omitempty"`
	Rounds           *int     `json:"rounds,omitempty"`
	Guardrails       []string `json:"guardrails,omitempty"`
	TurnFails        bool     `json:"turn_fails,omitempty"`
	MinWebSearches   int      `json:"min_web_searches,omitempty"`
	MinSources       int      `json:"min_sources,omitempty"`
	// SentToModelNotContains: none of these may appear in any request body
	// the model received (scripted suites only).
	SentToModelNotContains []string `json:"sent_to_model_not_contains,omitempty"`
	// ToolResultsInLastRequest: number of tool_result blocks in the final
	// message of the final request (scripted suites only).
	ToolResultsInLastRequest *int `json:"tool_results_in_last_request,omitempty"`

	// all suites
	Files map[string]FileExpect `json:"files,omitempty"`
	// Unchanged requires the workspace to be exactly as it started.
	Unchanged bool `json:"unchanged,omitempty"`
}

// FileExpect is what one file must look like afterwards.
type FileExpect struct {
	Equals   *string  `json:"equals,omitempty"`
	Contains []string `json:"contains,omitempty"`
	// ContainsAny passes if the file holds at least one of these. A task
	// often has more than one correct edit; this checks that one of them
	// was made without saying which.
	ContainsAny []string `json:"contains_any,omitempty"`
	NotContains []string `json:"not_contains,omitempty"`
	Absent      bool     `json:"absent,omitempty"`
}

// Report is the outcome of a run.
type Report struct {
	GeneratedAt time.Time     `json:"generated_at"`
	GitSHA      string        `json:"git_sha,omitempty"`
	Model       string        `json:"model,omitempty"` // set when a live suite ran
	Suites      []SuiteResult `json:"suites"`
}

// SuiteResult is the outcome of one suite. Ran is false for live suites
// that were skipped, so a report never implies a result it does not have.
type SuiteResult struct {
	Name        string       `json:"name"`
	Description string       `json:"description"`
	Kind        string       `json:"kind"`
	Ran         bool         `json:"ran"`
	Passed      int          `json:"passed"`
	Failed      int          `json:"failed"`
	Total       int          `json:"total"`
	Cases       []CaseResult `json:"cases"`

	// Where a live result came from. A live run costs money, so it happens
	// on demand and its result outlives the commit it ran at: a later run
	// without -live keeps it (see KeepLive) instead of erasing it, and these
	// fields say when, on which model and at which commit it was produced.
	RanAt  *time.Time `json:"ran_at,omitempty"`
	Model  string     `json:"model,omitempty"`
	GitSHA string     `json:"git_sha,omitempty"`
	// CasesHash identifies the case definitions the result is for. A kept
	// result is dropped as soon as a case is added, removed or reworded.
	CasesHash string `json:"cases_hash,omitempty"`
}

// CaseResult is the outcome of one case.
type CaseResult struct {
	ID         string   `json:"id"`
	Category   string   `json:"category"`
	Why        string   `json:"why"`
	Prompt     string   `json:"prompt,omitempty"`
	Ran        bool     `json:"ran"`
	Passed     bool     `json:"passed"`
	Failures   []string `json:"failures,omitempty"`
	Tools      []string `json:"tools,omitempty"`
	Rounds     int      `json:"rounds,omitempty"`
	CostUSD    float64  `json:"cost_usd,omitempty"`
	DurationMS int64    `json:"duration_ms"`
}

// LoadSuites reads every *.json suite in dir, ordered tool, scripted, live.
func LoadSuites(dir string) ([]Suite, error) {
	paths, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		return nil, err
	}
	var suites []Suite
	for _, p := range paths {
		raw, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}
		var s Suite
		dec := json.NewDecoder(strings.NewReader(string(raw)))
		dec.DisallowUnknownFields() // a misspelt expectation must not pass silently
		if err := dec.Decode(&s); err != nil {
			return nil, fmt.Errorf("%s: %w", p, err)
		}
		seen := map[string]bool{}
		for _, c := range s.Cases {
			if c.ID == "" || seen[c.ID] {
				return nil, fmt.Errorf("%s: missing or duplicate case id %q", p, c.ID)
			}
			seen[c.ID] = true
		}
		suites = append(suites, s)
	}
	order := map[string]int{"tool": 0, "scripted": 1, "live": 2}
	sort.SliceStable(suites, func(i, j int) bool { return order[suites[i].Kind] < order[suites[j].Kind] })
	return suites, nil
}

// Options controls a run.
type Options struct {
	// Live is the real model client. If nil, live suites are reported as
	// not run.
	Live      llm.Client
	LiveModel string
}

// Run executes the suites and returns a report.
func Run(ctx context.Context, suites []Suite, opts Options) Report {
	report := Report{GeneratedAt: time.Now().UTC()}
	for _, s := range suites {
		sr := SuiteResult{Name: s.Name, Description: s.Description, Kind: s.Kind, Total: len(s.Cases)}
		sr.Ran = s.Kind != "live" || opts.Live != nil
		if s.Kind == "live" {
			sr.CasesHash = casesHash(s.Cases)
			if sr.Ran {
				report.Model = opts.LiveModel
				at := report.GeneratedAt
				sr.Model, sr.RanAt = opts.LiveModel, &at
			}
		}
		for _, c := range s.Cases {
			cr := CaseResult{ID: c.ID, Category: c.Category, Why: c.Why, Prompt: c.Prompt, Ran: sr.Ran}
			if sr.Ran {
				start := time.Now()
				switch s.Kind {
				case "tool":
					runToolCase(ctx, c, &cr)
				case "scripted":
					runScriptedCase(ctx, c, &cr)
				case "live":
					runAgentCase(ctx, c, opts.Live, opts.LiveModel, nil, &cr)
				default:
					cr.Failures = append(cr.Failures, "unknown suite kind "+s.Kind)
				}
				cr.DurationMS = time.Since(start).Milliseconds()
				cr.Passed = len(cr.Failures) == 0
				if cr.Passed {
					sr.Passed++
				} else {
					sr.Failed++
				}
			}
			sr.Cases = append(sr.Cases, cr)
		}
		report.Suites = append(report.Suites, sr)
	}
	return report
}

// KeepLive fills in live suites this run skipped with the result of the last
// run that did not skip them, as long as the cases are the same ones. Without
// it, the free run that every commit makes would wipe the live result off the
// website. It returns the names of the suites it kept.
func (r *Report) KeepLive(prev Report) []string {
	var kept []string
	for i, cur := range r.Suites {
		if cur.Kind != "live" || cur.Ran {
			continue
		}
		for _, old := range prev.Suites {
			if old.Name != cur.Name || old.Kind != "live" || !old.Ran || !sameCases(cur, old) {
				continue
			}
			old.Description, old.CasesHash = cur.Description, cur.CasesHash
			if old.RanAt == nil { // a report written before suites carried their own date
				at := prev.GeneratedAt
				old.RanAt = &at
			}
			if old.Model == "" {
				old.Model = prev.Model
			}
			if old.GitSHA == "" {
				old.GitSHA = prev.GitSHA
			}
			r.Suites[i] = old
			kept = append(kept, old.Name)
			break
		}
	}
	return kept
}

func sameCases(cur, old SuiteResult) bool {
	if old.CasesHash != "" {
		return old.CasesHash == cur.CasesHash
	}
	// Older reports have no hash; fall back to the same cases in the same order.
	if len(cur.Cases) != len(old.Cases) {
		return false
	}
	for i := range cur.Cases {
		if cur.Cases[i].ID != old.Cases[i].ID || cur.Cases[i].Prompt != old.Cases[i].Prompt {
			return false
		}
	}
	return true
}

func casesHash(cases []Case) string {
	raw, err := json.Marshal(cases)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:8])
}

// Failed reports whether any case that ran did not pass.
func (r Report) Failed() bool {
	for _, s := range r.Suites {
		if s.Failed > 0 {
			return true
		}
	}
	return false
}

// ---- runners --------------------------------------------------------------

func seed(c Case) (map[string]string, *workspace.MemFS, error) {
	files := map[string]string{}
	for name, content := range c.Files {
		files[name] = expand(content)
	}
	for i := 0; i < c.FillerFiles; i++ {
		files[fmt.Sprintf("filler/file-%02d.txt", i)] = "filler\n"
	}
	fs, err := workspace.NewMemFS(workspace.DemoLimits, files)
	return files, fs, err
}

func runToolCase(ctx context.Context, c Case, cr *CaseResult) {
	before, fs, err := seed(c)
	if err != nil {
		cr.Failures = append(cr.Failures, "seed: "+err.Error())
		return
	}
	tool, ok := tools.Default().Get(c.Tool)
	if !ok {
		cr.Failures = append(cr.Failures, "no such tool: "+c.Tool)
		return
	}
	cr.Tools = []string{c.Tool}
	out, err := tool.Run(ctx, fs, json.RawMessage(expand(string(c.Input))))

	if want := c.Expect.ErrorContains; want != "" {
		if err == nil {
			cr.failf("expected an error containing %q, but the tool succeeded with %q", want, clip(out))
		} else if !strings.Contains(err.Error(), want) {
			cr.failf("expected an error containing %q, got %q", want, err.Error())
		}
	} else if err != nil {
		cr.failf("unexpected error: %v", err)
	}
	for _, want := range c.Expect.OutputContains {
		if !strings.Contains(out, want) {
			cr.failf("output does not contain %q: %q", want, clip(out))
		}
	}
	checkFiles(c.Expect, before, fs, cr)
}

func runScriptedCase(ctx context.Context, c Case, cr *CaseResult) {
	fake := fakellm.New(fakellm.Script(c.Script...))
	baseURL, closeFake, err := fake.Listen()
	if err != nil {
		cr.Failures = append(cr.Failures, "start fake model: "+err.Error())
		return
	}
	defer closeFake()
	client := llm.NewAnthropic(option.WithBaseURL(baseURL), option.WithAPIKey("eval"), option.WithMaxRetries(0))
	runAgentCase(ctx, c, client, "scripted", fake, cr)
}

func runAgentCase(ctx context.Context, c Case, client llm.Client, model string, fake *fakellm.Server, cr *CaseResult) {
	before, fs, err := seed(c)
	if err != nil {
		cr.Failures = append(cr.Failures, "seed: "+err.Error())
		return
	}
	research := c.Mode == "research"
	cfg := agent.Config{
		Model:     model,
		System:    agent.SystemPrompt(research, false),
		MaxRounds: c.MaxRounds,
		WebSearch: research,
		Now:       time.Now,
	}
	if c.Approve == "deny" {
		cfg.Approve = func(context.Context, agent.ToolEvent) (bool, error) { return false, nil }
	}
	ag := agent.New(client, tools.Default(), cfg)

	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	_, res, err := ag.Turn(ctx, nil, fs, c.Prompt, nil)

	e := c.Expect
	cr.Rounds, cr.CostUSD = res.Rounds, res.Usage.CostUSD
	for _, tc := range res.ToolCalls {
		cr.Tools = append(cr.Tools, tc.Name)
	}
	if e.TurnFails {
		if err == nil {
			cr.failf("expected the turn to fail, but it succeeded")
		}
	} else if err != nil {
		cr.failf("turn failed: %v", err)
		return
	}

	reply := strings.ToLower(res.Text)
	for _, want := range e.ReplyContains {
		if !strings.Contains(reply, strings.ToLower(want)) {
			cr.failf("reply does not contain %q: %q", want, clip(res.Text))
		}
	}
	if len(e.ReplyContainsAny) > 0 {
		found := false
		for _, want := range e.ReplyContainsAny {
			found = found || strings.Contains(reply, strings.ToLower(want))
		}
		if !found {
			cr.failf("reply contains none of %q: %q", e.ReplyContainsAny, clip(res.Text))
		}
	}
	for _, bad := range e.ReplyNotContains {
		if strings.Contains(reply, strings.ToLower(expand(bad))) {
			cr.failf("reply contains %q", bad)
		}
	}
	if !subsequence(e.ToolsCalled, cr.Tools) {
		cr.failf("expected tool calls %v in order, got %v", e.ToolsCalled, cr.Tools)
	}
	for _, bad := range e.ToolsNotCalled {
		for _, got := range cr.Tools {
			if got == bad {
				cr.failf("tool %s was called", bad)
			}
		}
	}
	if e.ToolErrors != nil {
		n := 0
		for _, tc := range res.ToolCalls {
			if tc.IsError {
				n++
			}
		}
		if n != *e.ToolErrors {
			cr.failf("expected %d tool errors, got %d", *e.ToolErrors, n)
		}
	}
	if e.Rounds != nil && res.Rounds != *e.Rounds {
		cr.failf("expected %d rounds, got %d", *e.Rounds, res.Rounds)
	}
	for _, want := range e.Guardrails {
		found := false
		for _, g := range res.Guardrails {
			found = found || g.Kind == want
		}
		if !found {
			cr.failf("guardrail %q did not fire (fired: %v)", want, guardrailKinds(res))
		}
	}
	if res.Usage.WebSearches < int64(e.MinWebSearches) {
		cr.failf("expected at least %d web searches, got %d", e.MinWebSearches, res.Usage.WebSearches)
	}
	if len(res.Sources) < e.MinSources {
		cr.failf("expected at least %d sources, got %d", e.MinSources, len(res.Sources))
	}

	if fake != nil {
		requests := fake.Requests()
		if len(e.SentToModelNotContains) > 0 {
			raw, _ := json.Marshal(requests)
			for _, bad := range e.SentToModelNotContains {
				if strings.Contains(string(raw), expand(bad)) {
					cr.failf("a request to the model contained %q", bad)
				}
			}
		}
		if e.ToolResultsInLastRequest != nil && len(requests) > 0 {
			last := requests[len(requests)-1]
			n := 0
			if len(last.Messages) > 0 {
				for _, b := range last.Messages[len(last.Messages)-1].Content {
					if b.Type == "tool_result" {
						n++
					}
				}
			}
			if n != *e.ToolResultsInLastRequest {
				cr.failf("expected %d tool results in the last request, got %d", *e.ToolResultsInLastRequest, n)
			}
		}
	}
	checkFiles(e, before, fs, cr)
}

// ---- checks ---------------------------------------------------------------

func checkFiles(e Expect, before map[string]string, fs *workspace.MemFS, cr *CaseResult) {
	after := fs.Snapshot()
	if e.Unchanged {
		if len(after) != len(before) {
			cr.failf("workspace changed: %d files before, %d after", len(before), len(after))
		}
		for name, content := range before {
			if after[name] != content {
				cr.failf("workspace changed: %s was modified or removed", name)
			}
		}
	}
	names := make([]string, 0, len(e.Files))
	for name := range e.Files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		want := e.Files[name]
		got, exists := after[name]
		if want.Absent {
			if exists {
				cr.failf("%s exists but should not", name)
			}
			continue
		}
		if !exists {
			cr.failf("%s does not exist", name)
			continue
		}
		if want.Equals != nil && got != expand(*want.Equals) {
			cr.failf("%s: content is %q, want %q", name, clip(got), clip(*want.Equals))
		}
		for _, s := range want.Contains {
			if !strings.Contains(got, s) {
				cr.failf("%s does not contain %q: %q", name, s, clipFile(got))
			}
		}
		if len(want.ContainsAny) > 0 && !slices.ContainsFunc(want.ContainsAny, func(s string) bool { return strings.Contains(got, s) }) {
			cr.failf("%s contains none of %q: %q", name, want.ContainsAny, clipFile(got))
		}
		for _, s := range want.NotContains {
			if strings.Contains(got, s) {
				cr.failf("%s contains %q: %q", name, s, clipFile(got))
			}
		}
	}
}

func (cr *CaseResult) failf(format string, args ...any) {
	cr.Failures = append(cr.Failures, fmt.Sprintf(format, args...))
}

func subsequence(want, got []string) bool {
	i := 0
	for _, g := range got {
		if i < len(want) && g == want[i] {
			i++
		}
	}
	return i == len(want)
}

func guardrailKinds(res agent.Result) []string {
	out := []string{}
	for _, g := range res.Guardrails {
		out = append(out, g.Kind)
	}
	return out
}

func clip(s string) string {
	if len(s) > 200 {
		return s[:200] + "…"
	}
	return s
}

// clipFile shows both ends of a file: an edit is as likely to be on the last
// line as on the first.
func clipFile(s string) string {
	if len(s) > 800 {
		return s[:400] + "\n…\n" + s[len(s)-400:]
	}
	return s
}

var repeatRe = regexp.MustCompile(`\{\{repeat:([^:}]+):(\d+)\}\}`)

// expand replaces {{repeat:TEXT:N}} with TEXT repeated N times, so suites can
// describe large inputs without containing them, and {{nul}} with a NUL.
func expand(s string) string {
	// {{nul}} becomes the JSON escape for a NUL byte, spelled indirectly so
	// this source file does not itself contain the escape.
	s = strings.ReplaceAll(s, "{{nul}}", string(rune(92))+"u0000")
	return repeatRe.ReplaceAllStringFunc(s, func(m string) string {
		parts := repeatRe.FindStringSubmatch(m)
		n, _ := strconv.Atoi(parts[2])
		return strings.Repeat(parts[1], n)
	})
}
