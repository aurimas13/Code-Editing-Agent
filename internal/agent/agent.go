// Package agent is the loop: send the conversation to the model, run any
// tools it asks for, send the results back, repeat until it answers in text.
//
// That is the whole idea from the tutorial, and it is still about forty
// lines (see Turn). What this package adds is everything a loop needs before
// strangers can use it: a bound on rounds, a place to approve writes,
// redaction and size limits on tool output, cost accounting, and an event
// for every step so the loop can be watched, stored, and tested.
package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/aurimas13/Code-Editing-Agent/internal/guardrails"
	"github.com/aurimas13/Code-Editing-Agent/internal/llm"
	"github.com/aurimas13/Code-Editing-Agent/internal/tools"
	"github.com/aurimas13/Code-Editing-Agent/internal/workspace"
)

// Config sets the agent's behaviour and limits.
type Config struct {
	Model     string
	System    string
	MaxTokens int64 // output tokens per model call
	// MaxRounds bounds model calls per turn. On the final round the model is
	// told to answer without tools, so a turn cannot loop forever and still
	// ends with something for the user.
	MaxRounds int
	// MaxToolResultBytes bounds what one tool call may add to the context.
	MaxToolResultBytes int
	// WebSearch enables the server-side search tool, capped at
	// WebSearchMaxUses searches per model call.
	WebSearch        bool
	WebSearchMaxUses int64
	// Approve, if set, is asked before any tool that changes the workspace
	// runs. Returning false sends a refusal back to the model.
	Approve func(ctx context.Context, call ToolEvent) (bool, error)
	// Now, if set, puts today's date at the end of the system prompt. The
	// model has no clock: without a date it cannot tell that a search
	// result is from last winter.
	Now func() time.Time
}

// Defaults fills unset fields with conservative values.
func (c Config) Defaults() Config {
	if c.Model == "" {
		c.Model = "claude-haiku-4-5"
	}
	if c.MaxTokens == 0 {
		c.MaxTokens = 2048
	}
	if c.MaxRounds == 0 {
		c.MaxRounds = 10
	}
	if c.MaxToolResultBytes == 0 {
		c.MaxToolResultBytes = 24 << 10
	}
	if c.WebSearchMaxUses == 0 {
		c.WebSearchMaxUses = 3
	}
	return c
}

// Agent runs turns. It holds no conversation state; callers own the history
// and the workspace, so one Agent serves every session.
type Agent struct {
	llm   llm.Client
	tools *tools.Registry
	cfg   Config
}

// New builds an agent.
func New(client llm.Client, registry *tools.Registry, cfg Config) *Agent {
	return &Agent{llm: client, tools: registry, cfg: cfg.Defaults()}
}

// Config returns the effective configuration.
func (a *Agent) Config() Config { return a.cfg }

// system is the system prompt for a model call, with today's date if the
// agent was given a clock.
func (a *Agent) system() string {
	if a.cfg.Now == nil || a.cfg.System == "" {
		return a.cfg.System
	}
	return a.cfg.System + "\n\nToday's date is " + a.cfg.Now().UTC().Format("Monday, 2 January 2006") + " (UTC)."
}

// Turn handles one user message. It returns the extended conversation and a
// summary. If the model call fails, the original history is returned
// unchanged so the session stays valid; file changes already made by tools
// in earlier rounds are kept.
func (a *Agent) Turn(
	ctx context.Context,
	history []anthropic.MessageParam,
	fs workspace.FS,
	input string,
	emit func(Event),
) ([]anthropic.MessageParam, Result, error) {
	if emit == nil {
		emit = func(Event) {}
	}
	var res Result
	guard := func(round int, kind, detail string) {
		g := Guardrail{Kind: kind, Detail: detail}
		res.Guardrails = append(res.Guardrails, g)
		emit(Event{Type: EventGuardrail, Round: round, Guardrail: &g})
	}

	conv := append(slices.Clone(history), anthropic.NewUserMessage(anthropic.NewTextBlock(input)))
	var texts []string

	for round := 1; ; round++ {
		// Last permitted round after tool use: force a text answer.
		final := round >= a.cfg.MaxRounds && round > 1
		if final {
			guard(round, "max_rounds", fmt.Sprintf("round limit of %d reached; answering without tools", a.cfg.MaxRounds))
		}

		emit(Event{Type: EventModelStart, Round: round})
		// Text is shown as it streams, so it is redacted as it streams.
		var streamed guardrails.StreamRedactor
		var streamedBytes int
		msg, err := a.llm.Generate(ctx, llm.Request{
			Model:     a.cfg.Model,
			System:    a.system(),
			Messages:  conv,
			Tools:     a.toolParams(),
			MaxTokens: a.cfg.MaxTokens,
			NoTools:   final,
		}, func(delta string) {
			streamedBytes += len(delta)
			if safe := streamed.Write(delta); safe != "" {
				emit(Event{Type: EventTextDelta, Round: round, Text: safe})
			}
		})
		if err != nil {
			// A call that fails or is cancelled part-way was still billed
			// for its input and for whatever it had generated.
			if msg != nil {
				out := msg.Usage.OutputTokens
				if out <= 1 {
					out = int64(streamedBytes/4) + 1 // the final count never arrived; estimate
				}
				a.addUsage(&res, msg.Usage.InputTokens, out, msg.Usage.ServerToolUse.WebSearchRequests)
			}
			return history, res, fmt.Errorf("model call failed: %w", err)
		}
		if tail := streamed.Flush(); tail != "" {
			emit(Event{Type: EventTextDelta, Round: round, Text: tail})
		}

		res.Rounds = round
		res.StopReason = string(msg.StopReason)
		a.addUsage(&res, msg.Usage.InputTokens, msg.Usage.OutputTokens, msg.Usage.ServerToolUse.WebSearchRequests)
		usage := res.Usage
		emit(Event{Type: EventUsage, Round: round, Usage: &usage})

		// A refusal, or a reply with nothing in it, must not be kept: the API
		// rejects a conversation containing an empty assistant message, and
		// a refused exchange should not be sent back as context. The turn is
		// dropped and the session stays usable.
		if msg.StopReason == anthropic.StopReasonRefusal || len(msg.Content) == 0 {
			kind, detail := "empty_reply", "the model returned nothing; this message was not added to the conversation"
			if msg.StopReason == anthropic.StopReasonRefusal {
				kind, detail = "model_refusal", "the model declined; this message was not added to the conversation"
			}
			guard(round, kind, detail)
			conv = history
			break
		}

		conv = append(conv, redactedParam(msg))

		// The API splits a cited sentence into several text blocks, one per
		// cited span, so neighbouring text blocks are one piece of prose.
		// They are joined before being redacted, shown and stored; a tool
		// call between two blocks is what separates one passage from the next.
		var passage strings.Builder
		flushText := func() {
			if passage.Len() == 0 {
				return
			}
			text, kinds := guardrails.Redact(passage.String())
			passage.Reset()
			if len(kinds) > 0 {
				guard(round, "secret_redacted", "in reply: "+strings.Join(kinds, ", "))
			}
			if strings.TrimSpace(text) != "" {
				texts = append(texts, text)
				emit(Event{Type: EventText, Round: round, Text: text})
			}
		}

		var toolResults []anthropic.ContentBlockParamUnion
		for _, block := range msg.Content {
			if block.Type != "text" {
				flushText()
			}
			switch block.Type {
			case "text":
				passage.WriteString(block.Text)
				for _, c := range block.Citations {
					if c.URL != "" {
						res.Sources = addSource(res.Sources, Source{URL: c.URL, Title: c.Title, Cited: true})
					}
				}
			case "tool_use":
				// A tool call cut off by the output limit has incomplete
				// input. It still needs a result or the conversation is
				// invalid, so it gets an error instead of being run.
				truncated := msg.StopReason != anthropic.StopReasonToolUse
				result, ev := a.runTool(ctx, fs, round, block, truncated, emit, guard)
				res.ToolCalls = append(res.ToolCalls, ev)
				toolResults = append(toolResults, result)
			case "server_tool_use":
				var in struct {
					Query string `json:"query"`
				}
				_ = json.Unmarshal(block.Input, &in)
				emit(Event{Type: EventWebSearch, Round: round, Text: in.Query})
			case "web_search_tool_result":
				for _, r := range block.Content.OfWebSearchResultBlockArray {
					res.Sources = addSource(res.Sources, Source{URL: r.URL, Title: r.Title})
				}
				if code := block.Content.ErrorCode; code != "" {
					guard(round, "web_search_error", string(code))
				}
			}
		}
		flushText()

		if msg.StopReason == anthropic.StopReasonMaxTokens {
			guard(round, "output_truncated", fmt.Sprintf("reply hit the %d-token output limit", a.cfg.MaxTokens))
		}

		if len(toolResults) > 0 {
			conv = append(conv, anthropic.NewUserMessage(toolResults...))
			if round >= a.cfg.MaxRounds {
				break
			}
			continue
		}
		// pause_turn: a server-side tool is still working; ask again.
		if msg.StopReason == anthropic.StopReasonPauseTurn {
			if round < a.cfg.MaxRounds {
				continue
			}
			// Out of rounds with a search still open. Keeping the half-done
			// exchange would leave a tool call without its result.
			guard(round, "max_rounds", "round limit reached while a web search was still running; this message was not added to the conversation")
			conv = history
		}
		break
	}

	// The turn is over: what later turns need is what was said, not the
	// pages that were read to say it.
	if len(conv) > len(history) {
		compactSearches(conv[len(history):])
	}

	res.Text = strings.Join(texts, "\n\n")
	if len(res.Sources) > 0 {
		emit(Event{Type: EventSources, Round: res.Rounds, Sources: res.Sources})
	}
	emit(Event{Type: EventDone, Round: res.Rounds, StopReason: res.StopReason})
	return conv, res, nil
}

// runTool executes one tool call and returns the block to send back to the
// model. Every failure becomes an error result the model can read and react
// to; nothing here ends the turn.
func (a *Agent) runTool(
	ctx context.Context,
	fs workspace.FS,
	round int,
	block anthropic.ContentBlockUnion,
	truncated bool,
	emit func(Event),
	guard func(round int, kind, detail string),
) (anthropic.ContentBlockParamUnion, ToolEvent) {
	input := block.Input
	if len(input) == 0 {
		input = json.RawMessage(`{}`)
	}
	// After a web search the model marks quoted passages with <cite> tags.
	// In a reply the API turns those into citations; in a tool input they
	// are just text, and would be written into the user's file as markup.
	if a.cfg.WebSearch {
		if clean, changed := stripCitationTags(input); changed {
			input = clean
			guard(round, "citation_markup_removed", "from "+block.Name+" input")
		}
	}
	ev := ToolEvent{ID: block.ID, Name: block.Name, Input: displayInput(input)}
	call := ev
	emit(Event{Type: EventToolCall, Round: round, Tool: &call})

	start := time.Now()
	out, isErr := a.execute(ctx, fs, round, block.Name, input, truncated, ev, guard)

	out, kinds := guardrails.Redact(out)
	if len(kinds) > 0 {
		guard(round, "secret_redacted", "in "+block.Name+" result: "+strings.Join(kinds, ", "))
	}
	out, cut := guardrails.Truncate(out, a.cfg.MaxToolResultBytes)
	if cut {
		guard(round, "tool_result_truncated", block.Name)
	}

	ev.Output, ev.IsError, ev.DurationMS = out, isErr, time.Since(start).Milliseconds()
	done := ev
	emit(Event{Type: EventToolResult, Round: round, Tool: &done})
	return anthropic.NewToolResultBlock(block.ID, out, isErr), ev
}

func (a *Agent) execute(
	ctx context.Context,
	fs workspace.FS,
	round int,
	name string,
	input json.RawMessage,
	truncated bool,
	ev ToolEvent,
	guard func(round int, kind, detail string),
) (string, bool) {
	if truncated {
		return "Not executed: the request was cut off by the output-token limit. Send the tool call again, with smaller input if needed.", true
	}
	tool, ok := a.tools.Get(name)
	if !ok {
		return fmt.Sprintf("tool not found: %q. Available tools: %s", name, strings.Join(a.tools.Names(), ", ")), true
	}
	if !json.Valid(input) {
		return "invalid tool input: not valid JSON", true
	}
	if tool.Mutates && a.cfg.Approve != nil {
		approved, err := a.cfg.Approve(ctx, ev)
		if err != nil {
			return "approval failed: " + err.Error(), true
		}
		if !approved {
			guard(round, "approval_denied", name)
			return "The user declined this change. Do not retry it; ask what they would like instead.", true
		}
	}
	out, err := tool.Run(ctx, fs, input)
	if err != nil {
		if isBoundary(err) {
			guard(round, "workspace_boundary", err.Error())
		}
		return err.Error(), true
	}
	return out, false
}

func (a *Agent) addUsage(res *Result, in, out, searches int64) {
	res.Usage.InputTokens += in
	res.Usage.OutputTokens += out
	res.Usage.WebSearches += searches
	res.Usage.CostUSD = llm.Cost(a.cfg.Model, res.Usage.InputTokens, res.Usage.OutputTokens, res.Usage.WebSearches)
}

// redactedParam converts a reply into the form kept in the conversation,
// with credential-shaped text removed from its text blocks so the stored
// history matches what the visitor was shown.
func redactedParam(msg *anthropic.Message) anthropic.MessageParam {
	p := msg.ToParam()
	for i := range p.Content {
		if t := p.Content[i].OfText; t != nil {
			t.Text, _ = guardrails.Redact(t.Text)
		}
	}
	return p
}

// compactSearches rewrites the assistant messages of a finished turn so they
// hold the reply and the local tool calls, without the web search blocks.
//
// The API bills the whole conversation again on every call. Measured on the
// live site, each earlier search added about 8,500 input tokens to every
// later model call: a one-line question that cost 0.3 cents in a fresh
// session cost 1.8 cents after one search and 3.5 after two, and a session
// reached its context limit after a handful. The model keeps its own answer,
// which is what a follow-up question refers to; if it needs the pages again
// it can search again. Citations go with the results they point into, and neighbouring text
// blocks are merged, as they are for display.
func compactSearches(turn []anthropic.MessageParam) {
	for i := range turn {
		if turn[i].Role != anthropic.MessageParamRoleAssistant {
			continue
		}
		var kept []anthropic.ContentBlockParamUnion
		var passage strings.Builder
		searched, gap := false, false
		flush := func() {
			if strings.TrimSpace(passage.String()) != "" {
				kept = append(kept, anthropic.NewTextBlock(passage.String()))
			}
			passage.Reset()
			gap = false
		}
		for _, block := range turn[i].Content {
			switch {
			case block.OfServerToolUse != nil || block.OfWebSearchToolResult != nil:
				searched, gap = true, passage.Len() > 0
			case block.OfText != nil:
				if gap {
					passage.WriteString("\n\n")
					gap = false
				}
				passage.WriteString(block.OfText.Text)
			default:
				flush()
				kept = append(kept, block)
			}
		}
		flush()
		if !searched {
			continue // nothing to remove; leave the message exactly as it was
		}
		if len(kept) == 0 {
			// The API rejects an empty assistant message.
			kept = append(kept, anthropic.NewTextBlock("(Searched the web.)"))
		}
		turn[i].Content = kept
	}
}

func (a *Agent) toolParams() []anthropic.ToolUnionParam {
	params := a.tools.Params()
	if a.cfg.WebSearch {
		params = append(params, anthropic.ToolUnionParam{
			OfWebSearchTool20250305: &anthropic.WebSearchTool20250305Param{
				MaxUses: anthropic.Int(a.cfg.WebSearchMaxUses),
			},
		})
	}
	return params
}

// displayInput is the tool input as shown in events and stored in traces.
// The tool itself receives the original.
func displayInput(input json.RawMessage) json.RawMessage {
	red, kinds := guardrails.Redact(string(input))
	if len(kinds) == 0 {
		return input
	}
	if json.Valid([]byte(red)) {
		return json.RawMessage(red)
	}
	return json.RawMessage(`{"redacted":true}`)
}

func isBoundary(err error) bool {
	return errors.Is(err, workspace.ErrEscape) ||
		errors.Is(err, workspace.ErrSensitive) ||
		errors.Is(err, workspace.ErrQuota)
}

func addSource(list []Source, s Source) []Source {
	for i := range list {
		if list[i].URL == s.URL {
			list[i].Cited = list[i].Cited || s.Cited
			if list[i].Title == "" {
				list[i].Title = s.Title
			}
			return list
		}
	}
	return append(list, s)
}

var citationTag = regexp.MustCompile(`</?cite\b[^>]*>`)

// stripCitationTags removes <cite ...> and </cite> from every string in a
// tool input, keeping the text between them. Input that is not a JSON object
// is returned as it came; the tool will report what is wrong with it.
func stripCitationTags(input json.RawMessage) (json.RawMessage, bool) {
	if !bytes.Contains(input, []byte("cite")) {
		return input, false
	}
	var fields map[string]any
	if err := json.Unmarshal(input, &fields); err != nil {
		return input, false
	}
	changed := false
	for k, v := range fields {
		if str, ok := v.(string); ok {
			if clean := citationTag.ReplaceAllString(str, ""); clean != str {
				fields[k], changed = clean, true
			}
		}
	}
	if !changed {
		return input, false
	}
	out, err := json.Marshal(fields)
	if err != nil {
		return input, false
	}
	return out, true
}
