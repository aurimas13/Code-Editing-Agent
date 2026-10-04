// Package fakellm is a stand-in for the Anthropic Messages API.
//
// It speaks the real wire format, including server-sent events, so the SDK
// client, the streaming accumulator, and the agent loop all run unmodified
// against it. Tests and evals use it with a fixed script of responses; the
// demo mode uses it with a small rule-based responder so the site and the
// CLI work without an API key.
package fakellm

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Block is one content block of a scripted assistant message.
type Block struct {
	// Type is "text" or "tool_use", or, to imitate the server-side search
	// tool, "server_tool_use" and "web_search_tool_result".
	Type  string          `json:"type"`
	Text  string          `json:"text,omitempty"`
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
	// Citations attach web sources to a text block.
	Citations []Citation `json:"citations,omitempty"`
	// ToolUseID and Results describe a web_search_tool_result.
	ToolUseID string         `json:"tool_use_id,omitempty"`
	Results   []SearchResult `json:"results,omitempty"`
}

// SearchResult is one page returned by the imitated web search.
type SearchResult struct {
	URL   string `json:"url"`
	Title string `json:"title"`
}

// Citation is a web source cited by a text block.
type Citation struct {
	URL       string `json:"url"`
	Title     string `json:"title"`
	CitedText string `json:"cited_text"`
}

// Response is a scripted assistant message.
type Response struct {
	Blocks []Block `json:"blocks"`
	// StopReason defaults to "tool_use" when the response contains a tool
	// call and "end_turn" otherwise.
	StopReason string `json:"stop_reason,omitempty"`
	// Status, when non-zero, makes the fake return an API error instead.
	Status int `json:"status,omitempty"`
}

// Text builds a plain-text response.
func Text(s string) Response {
	return Response{Blocks: []Block{{Type: "text", Text: s}}}
}

// ToolUse builds a response that calls one tool, optionally after some text.
func ToolUse(id, name, inputJSON string, preamble ...string) Response {
	var blocks []Block
	for _, p := range preamble {
		blocks = append(blocks, Block{Type: "text", Text: p})
	}
	blocks = append(blocks, Block{Type: "tool_use", ID: id, Name: name, Input: json.RawMessage(inputJSON)})
	return Response{Blocks: blocks}
}

// Request is the decoded body of a call to POST /v1/messages.
type Request struct {
	Model      string            `json:"model"`
	MaxTokens  int64             `json:"max_tokens"`
	System     json.RawMessage   `json:"system"`
	Messages   []Message         `json:"messages"`
	Tools      []json.RawMessage `json:"tools"`
	ToolChoice struct {
		Type string `json:"type"`
	} `json:"tool_choice"`
	Stream bool `json:"stream"`
}

// Message is one conversation message as sent by the client.
type Message struct {
	Role    string         `json:"role"`
	Content []ContentBlock `json:"-"`
}

// ContentBlock is one block of a request message.
type ContentBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
	Content   json.RawMessage `json:"content"`
	IsError   bool            `json:"is_error"`
}

// UnmarshalJSON accepts content as either a string or an array of blocks.
func (m *Message) UnmarshalJSON(data []byte) error {
	var raw struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	m.Role = raw.Role
	var s string
	if json.Unmarshal(raw.Content, &s) == nil {
		m.Content = []ContentBlock{{Type: "text", Text: s}}
		return nil
	}
	return json.Unmarshal(raw.Content, &m.Content)
}

// ResultText returns the text of a tool_result block.
func (b ContentBlock) ResultText() string {
	var s string
	if json.Unmarshal(b.Content, &s) == nil {
		return s
	}
	var parts []struct {
		Text string `json:"text"`
	}
	_ = json.Unmarshal(b.Content, &parts)
	var sb strings.Builder
	for _, p := range parts {
		sb.WriteString(p.Text)
	}
	return sb.String()
}

// Responder decides what the fake model says next.
type Responder func(Request) Response

// Script returns a responder that replays responses in order. If the client
// asks for more, it answers with a marker text so tests fail visibly.
func Script(responses ...Response) Responder {
	var mu sync.Mutex
	i := 0
	return func(Request) Response {
		mu.Lock()
		defer mu.Unlock()
		if i >= len(responses) {
			return Text("[script exhausted]")
		}
		r := responses[i]
		i++
		return r
	}
}

// Server is an http.Handler implementing the subset of the Messages API the
// agent uses.
type Server struct {
	respond Responder
	// ChunkDelay spaces out streamed text so a demo looks like typing.
	ChunkDelay time.Duration
	// IgnoreToolChoice makes the fake keep calling tools even when told to
	// answer in text, to test that the loop does not depend on obedience.
	IgnoreToolChoice bool

	mu       sync.Mutex
	requests []Request
}

// New returns a fake API driven by respond.
func New(respond Responder) *Server { return &Server{respond: respond} }

// Requests returns every request received so far.
func (s *Server) Requests() []Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Request(nil), s.requests...)
}

// Listen serves the fake on a loopback port and returns its base URL.
func (s *Server) Listen() (baseURL string, closeFn func(), err error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", nil, err
	}
	srv := &http.Server{Handler: s, ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = srv.Serve(ln) }()
	return "http://" + ln.Addr().String(), func() { _ = srv.Close() }, nil
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/v1/messages") {
		http.NotFound(w, r)
		return
	}
	var req Request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_request_error", err.Error())
		return
	}
	s.mu.Lock()
	s.requests = append(s.requests, req)
	s.mu.Unlock()

	resp := s.respond(req)
	if resp.Status != 0 {
		writeAPIError(w, resp.Status, "api_error", "scripted failure")
		return
	}
	// tool_choice "none" means the model must answer in text. Honour it the
	// way the real API does, so the round-limit path can be tested.
	if req.ToolChoice.Type == "none" && !s.IgnoreToolChoice {
		var kept []Block
		for _, b := range resp.Blocks {
			if b.Type != "tool_use" {
				kept = append(kept, b)
			}
		}
		if len(kept) == 0 {
			kept = []Block{{Type: "text", Text: "I've reached the step limit for this turn."}}
		}
		resp.Blocks, resp.StopReason = kept, "end_turn"
	}
	if resp.StopReason == "" {
		resp.StopReason = "end_turn"
		for _, b := range resp.Blocks {
			if b.Type == "tool_use" {
				resp.StopReason = "tool_use"
			}
		}
	}

	inTokens := int64(promptChars(req)/4 + 1)
	var outChars int
	for _, b := range resp.Blocks {
		outChars += len(b.Text) + len(b.Input)
	}
	outTokens := int64(outChars/4 + 1)

	var searches int64
	for _, b := range resp.Blocks {
		if b.Type == "server_tool_use" {
			searches++
		}
	}
	usage := map[string]any{"output_tokens": outTokens}
	if searches > 0 {
		usage["server_tool_use"] = map[string]any{"web_search_requests": searches, "web_fetch_requests": 0}
	}

	if req.Stream {
		s.stream(w, req, resp, inTokens, usage)
		return
	}
	content := make([]map[string]any, 0, len(resp.Blocks))
	for _, b := range resp.Blocks {
		content = append(content, blockJSON(b, true))
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"id": "msg_fake", "type": "message", "role": "assistant", "model": req.Model,
		"content": content, "stop_reason": resp.StopReason, "stop_sequence": nil,
		"usage": merge(usage, map[string]any{"input_tokens": inTokens}),
	})
}

func merge(a, b map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		out[k] = v
	}
	return out
}

// promptChars is a rough size of the request, for token estimates.
func promptChars(r Request) int {
	n := len(r.System)
	for _, m := range r.Messages {
		for _, b := range m.Content {
			n += len(b.Text) + len(b.Input) + len(b.Content)
		}
	}
	return n
}

func (s *Server) stream(w http.ResponseWriter, req Request, resp Response, inTokens int64, usage map[string]any) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	flusher, _ := w.(http.Flusher)
	send := func(event string, data any) {
		payload, _ := json.Marshal(data)
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, payload)
		if flusher != nil {
			flusher.Flush()
		}
	}

	send("message_start", map[string]any{"type": "message_start", "message": map[string]any{
		"id": "msg_fake", "type": "message", "role": "assistant", "model": req.Model,
		"content": []any{}, "stop_reason": nil, "stop_sequence": nil,
		"usage": map[string]any{"input_tokens": inTokens, "output_tokens": 1},
	}})
	for i, b := range resp.Blocks {
		send("content_block_start", map[string]any{
			"type": "content_block_start", "index": i, "content_block": blockJSON(b, false),
		})
		switch b.Type {
		case "text":
			for _, c := range b.Citations {
				send("content_block_delta", map[string]any{
					"type": "content_block_delta", "index": i,
					"delta": map[string]any{"type": "citations_delta", "citation": citationJSON(c)},
				})
			}
			for _, chunk := range chunks(b.Text, 14) {
				if s.ChunkDelay > 0 {
					time.Sleep(s.ChunkDelay)
				}
				send("content_block_delta", map[string]any{
					"type": "content_block_delta", "index": i,
					"delta": map[string]any{"type": "text_delta", "text": chunk},
				})
			}
		case "tool_use", "server_tool_use":
			input := string(b.Input)
			if input == "" {
				input = "{}"
			}
			// Split the JSON in two to exercise partial-input accumulation.
			mid := len(input) / 2
			for _, part := range []string{input[:mid], input[mid:]} {
				send("content_block_delta", map[string]any{
					"type": "content_block_delta", "index": i,
					"delta": map[string]any{"type": "input_json_delta", "partial_json": part},
				})
			}
		}
		send("content_block_stop", map[string]any{"type": "content_block_stop", "index": i})
	}
	send("message_delta", map[string]any{
		"type":  "message_delta",
		"delta": map[string]any{"stop_reason": resp.StopReason, "stop_sequence": nil},
		"usage": usage,
	})
	send("message_stop", map[string]any{"type": "message_stop"})
}

// blockJSON renders a block. In a stream, content_block_start carries an
// empty shell and the deltas fill it; in a plain response it is complete.
// A web search result always arrives whole, as it does from the real API.
func blockJSON(b Block, complete bool) map[string]any {
	switch b.Type {
	case "tool_use", "server_tool_use":
		var input any = map[string]any{}
		if complete && len(b.Input) > 0 {
			_ = json.Unmarshal(b.Input, &input)
		}
		return map[string]any{"type": b.Type, "id": b.ID, "name": b.Name, "input": input}
	case "web_search_tool_result":
		results := make([]map[string]any, 0, len(b.Results))
		for _, r := range b.Results {
			results = append(results, map[string]any{
				"type": "web_search_result", "url": r.URL, "title": r.Title,
				"encrypted_content": "ZmFrZQ==", "page_age": nil,
			})
		}
		return map[string]any{"type": "web_search_tool_result", "tool_use_id": b.ToolUseID, "content": results}
	default:
		out := map[string]any{"type": "text", "text": ""}
		if complete {
			out["text"] = b.Text
			if len(b.Citations) > 0 {
				cites := make([]map[string]any, 0, len(b.Citations))
				for _, c := range b.Citations {
					cites = append(cites, citationJSON(c))
				}
				out["citations"] = cites
			}
		}
		return out
	}
}

func citationJSON(c Citation) map[string]any {
	return map[string]any{
		"type": "web_search_result_location", "url": c.URL, "title": c.Title,
		"cited_text": c.CitedText, "encrypted_index": "ZmFrZQ==",
	}
}

func chunks(s string, size int) []string {
	runes := []rune(s)
	var out []string
	for len(runes) > 0 {
		n := min(size, len(runes))
		out = append(out, string(runes[:n]))
		runes = runes[n:]
	}
	return out
}

func writeAPIError(w http.ResponseWriter, status int, kind, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"type": "error", "error": map[string]any{"type": kind, "message": msg},
	})
}
