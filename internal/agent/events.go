package agent

import "encoding/json"

// Event types, in the order they typically occur within a turn.
const (
	EventModelStart = "model_start" // a request to the model is about to be sent
	EventTextDelta  = "text_delta"  // a piece of the reply, as it streams
	EventText       = "text"        // one complete text block of the reply
	EventToolCall   = "tool_call"   // the model asked for a tool
	EventToolResult = "tool_result" // our code ran the tool; this is what it returned
	EventWebSearch  = "web_search"  // the model ran a server-side web search
	EventSources    = "sources"     // pages returned by web search
	EventGuardrail  = "guardrail"   // a limit or check intervened
	EventUsage      = "usage"       // cumulative tokens and cost for the turn
	EventDone       = "done"        // the turn is over
)

// Event is one observable step of a turn. The CLI prints events, the HTTP
// server streams them to the browser, and the store saves them as the trace.
// Everything the agent does is visible through this one type.
type Event struct {
	Type       string     `json:"type"`
	Round      int        `json:"round,omitempty"`
	Text       string     `json:"text,omitempty"`
	Tool       *ToolEvent `json:"tool,omitempty"`
	Guardrail  *Guardrail `json:"guardrail,omitempty"`
	Sources    []Source   `json:"sources,omitempty"`
	Usage      *Usage     `json:"usage,omitempty"`
	StopReason string     `json:"stop_reason,omitempty"`
}

// ToolEvent describes a tool call and, once it has run, its result.
type ToolEvent struct {
	ID         string          `json:"id"`
	Name       string          `json:"name"`
	Input      json.RawMessage `json:"input,omitempty"`
	Output     string          `json:"output,omitempty"`
	IsError    bool            `json:"is_error,omitempty"`
	DurationMS int64           `json:"duration_ms,omitempty"`
}

// Guardrail records a check that fired.
type Guardrail struct {
	Kind   string `json:"kind"`
	Detail string `json:"detail,omitempty"`
}

// Source is a web page the model found or cited.
type Source struct {
	URL   string `json:"url"`
	Title string `json:"title,omitempty"`
	Cited bool   `json:"cited,omitempty"`
}

// Usage is token and cost accounting.
type Usage struct {
	InputTokens  int64   `json:"input_tokens"`
	OutputTokens int64   `json:"output_tokens"`
	WebSearches  int64   `json:"web_searches"`
	CostUSD      float64 `json:"cost_usd"`
}

// Result summarises a finished turn.
type Result struct {
	Text       string      `json:"text"`
	Rounds     int         `json:"rounds"`
	StopReason string      `json:"stop_reason"`
	Usage      Usage       `json:"usage"`
	ToolCalls  []ToolEvent `json:"tool_calls,omitempty"`
	Sources    []Source    `json:"sources,omitempty"`
	Guardrails []Guardrail `json:"guardrails,omitempty"`
}
