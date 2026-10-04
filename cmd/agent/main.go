// Command agent is the terminal version: the tutorial's chat loop, running
// on the refactored packages.
//
//	export ANTHROPIC_API_KEY=...
//	go run ./cmd/agent -dir ./some/project
//
// It can only see files inside -dir, skips files that usually hold secrets,
// and asks before every edit unless -yes is given.
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"

	"github.com/aurimas13/Code-Editing-Agent/internal/agent"
	"github.com/aurimas13/Code-Editing-Agent/internal/guardrails"
	"github.com/aurimas13/Code-Editing-Agent/internal/llm"
	"github.com/aurimas13/Code-Editing-Agent/internal/llm/fakellm"
	"github.com/aurimas13/Code-Editing-Agent/internal/tools"
	"github.com/aurimas13/Code-Editing-Agent/internal/workspace"
)

const (
	blue   = "\x1b[94m"
	yellow = "\x1b[93m"
	green  = "\x1b[92m"
	red    = "\x1b[91m"
	dim    = "\x1b[2m"
	reset  = "\x1b[0m"
)

func main() {
	dir := flag.String("dir", ".", "workspace directory the agent may read and edit")
	model := flag.String("model", "claude-haiku-4-5", "model ID")
	research := flag.Bool("research", false, "also give the agent web search")
	yes := flag.Bool("yes", false, "apply edits without asking")
	maxRounds := flag.Int("max-rounds", 12, "model calls allowed per message")
	demo := flag.Bool("demo", false, "use the scripted stand-in model (no API key needed)")
	flag.Parse()

	fs, err := workspace.OpenDiskFS(*dir, workspace.LocalLimits)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
	defer fs.Close()

	var client llm.Client
	switch {
	case *demo || os.Getenv("ANTHROPIC_API_KEY") == "":
		if !*demo {
			fmt.Println(dim + "ANTHROPIC_API_KEY is not set; running the scripted demo model." + reset)
		}
		baseURL, closeFake, err := fakellm.New(fakellm.DemoBrain).Listen()
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}
		defer closeFake()
		client = llm.NewAnthropic(option.WithBaseURL(baseURL), option.WithAPIKey("demo"), option.WithMaxRetries(0))
	default:
		client = llm.NewAnthropic()
	}

	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 0, 64<<10), 1<<20)
	readLine := func() (string, bool) {
		if !scanner.Scan() {
			return "", false
		}
		return scanner.Text(), true
	}

	cfg := agent.Config{
		Model:     *model,
		System:    agent.SystemPrompt(*research, false),
		MaxRounds: *maxRounds,
		WebSearch: *research,
		Now:       time.Now,
	}
	if !*yes {
		cfg.Approve = func(_ context.Context, call agent.ToolEvent) (bool, error) {
			fmt.Printf("%sapprove%s %s(%s)? [y/N] ", yellow, reset, call.Name, call.Input)
			answer, ok := readLine()
			return ok && strings.HasPrefix(strings.ToLower(strings.TrimSpace(answer)), "y"), nil
		}
	}
	ag := agent.New(client, tools.Default(), cfg)

	fmt.Printf("Chat with Claude (use 'ctrl-c' to quit). Workspace: %s\n", *dir)
	var conversation []anthropic.MessageParam
	for {
		fmt.Print(blue + "You" + reset + ": ")
		input, ok := readLine()
		if !ok {
			break
		}
		input, _, err := guardrails.CheckInput(input, 0)
		if err != nil {
			continue
		}
		input, _ = guardrails.Redact(input)

		streaming := false
		conversation, _, err = ag.Turn(context.Background(), conversation, fs, input, func(ev agent.Event) {
			switch ev.Type {
			case agent.EventTextDelta:
				if !streaming {
					fmt.Print(yellow + "Claude" + reset + ": ")
					streaming = true
				}
				fmt.Print(ev.Text)
			case agent.EventText:
				if streaming {
					fmt.Println()
				}
				streaming = false
			case agent.EventToolCall:
				fmt.Printf("%stool%s: %s(%s)\n", green, reset, ev.Tool.Name, ev.Tool.Input)
			case agent.EventToolResult:
				if ev.Tool.IsError {
					fmt.Printf("%s  ↳ error:%s %s\n", red, reset, ev.Tool.Output)
				}
			case agent.EventWebSearch:
				fmt.Printf("%ssearch%s: %s\n", green, reset, ev.Text)
			case agent.EventGuardrail:
				fmt.Printf("%sguardrail: %s %s%s\n", dim, ev.Guardrail.Kind, ev.Guardrail.Detail, reset)
			case agent.EventUsage:
				// printed once per turn, below
			case agent.EventDone:
			}
		})
		if err != nil {
			fmt.Printf("%sError:%s %s\n", red, reset, err)
		}
	}
}
