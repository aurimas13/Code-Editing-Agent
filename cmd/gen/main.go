// Command gen writes the data files the website is built from.
//
//	go run ./cmd/gen
//
// It copies the tutorial checkpoints (guide/steps) and the agent's real
// system prompt and limits into web/src/generated, so the site shows the
// code and configuration that actually ship, not a hand-copied version. CI
// runs it and fails if the committed files are out of date.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/aurimas13/Code-Editing-Agent/guide"
	"github.com/aurimas13/Code-Editing-Agent/internal/agent"
	"github.com/aurimas13/Code-Editing-Agent/internal/server"
	"github.com/aurimas13/Code-Editing-Agent/internal/tools"
	"github.com/aurimas13/Code-Editing-Agent/internal/workspace"
)

func main() {
	out := "web/src/generated"
	if len(os.Args) > 1 {
		out = os.Args[1]
	}
	cfg := server.DefaultConfig()

	type toolInfo struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Schema      any    `json:"schema"`
		Mutates     bool   `json:"mutates"`
	}
	var toolInfos []toolInfo
	for _, name := range tools.Default().Names() {
		t, _ := tools.Default().Get(name)
		toolInfos = append(toolInfos, toolInfo{t.Name, t.Description, t.InputSchema, t.Mutates})
	}

	write(filepath.Join(out, "guide.json"), map[string]any{"steps": guide.Steps()})
	write(filepath.Join(out, "agent.json"), map[string]any{
		"system_prompt_code":     agent.SystemPrompt(false, true) + agent.ResearchTabHint,
		"system_prompt_research": agent.SystemPrompt(true, true),
		"tools":                  toolInfos,
		"seed_files":             server.SeedFiles(),
		"limits": map[string]any{
			"model":                 cfg.Model,
			"max_input_chars":       cfg.MaxInputChars,
			"max_output_tokens":     cfg.MaxOutputTokens,
			"max_rounds":            cfg.MaxRounds,
			"web_search_max_uses":   cfg.WebSearchMaxUses,
			"turn_timeout_seconds":  cfg.TurnTimeout.Seconds(),
			"max_turns_per_session": cfg.MaxTurnsPerSession,
			"max_context_tokens":    cfg.MaxContextTokens,
			"session_ttl_minutes":   cfg.SessionTTL.Minutes(),
			"turns_per_minute":      cfg.TurnsPerMinute,
			"turns_per_day":         cfg.TurnsPerDay,
			"research_per_day":      cfg.ResearchPerDay,
			"sessions_per_hour":     cfg.SessionsPerHour,
			"requests_per_minute":   cfg.RequestsPerMinute,
			"daily_budget_usd":      cfg.DailyBudgetUSD,
			"max_concurrent_turns":  cfg.MaxConcurrentTurns,
			"workspace_max_files":   workspace.DemoLimits.MaxFiles,
			"workspace_max_file_kb": workspace.DemoLimits.MaxFileBytes >> 10,
			"workspace_max_kb":      workspace.DemoLimits.MaxTotalBytes >> 10,
		},
	})
}

func write(path string, v any) {
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		fail(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		fail(err)
	}
	if err := os.WriteFile(path, append(raw, '\n'), 0o644); err != nil {
		fail(err)
	}
	fmt.Println("wrote", path)
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "gen:", err)
	os.Exit(1)
}
