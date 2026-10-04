package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/aurimas13/Code-Editing-Agent/internal/agent"
	"github.com/aurimas13/Code-Editing-Agent/internal/guardrails"
	"github.com/aurimas13/Code-Editing-Agent/internal/store"
	"github.com/aurimas13/Code-Editing-Agent/internal/workspace"
)

// maxTraceOutput bounds a tool result as shown in the browser and stored in
// the trace. The model receives the fuller version (Config.MaxToolResultBytes).
const maxTraceOutput = 4 << 10

type messageRequest struct {
	Content string `json:"content"`
	Mode    string `json:"mode"`
}

// handleMessage runs one turn and streams it as server-sent events.
//
// Everything that can reject the request does so before the stream starts,
// with an ordinary JSON error and status code. Once the stream is open, the
// status is already 200, so failures are reported as an "error" event.
func (s *Server) handleMessage(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.authed(w, r)
	if !ok {
		return
	}
	ip := s.clientHash(r)

	// 1. Parse and validate input.
	var req messageRequest
	body := http.MaxBytesReader(w, r.Body, 16<<10)
	if err := json.NewDecoder(body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "Send JSON with a \"content\" field.")
		return
	}
	if req.Mode == "" {
		req.Mode = ModeCode
	}
	ag, known := s.agents[req.Mode]
	if !known {
		writeError(w, http.StatusBadRequest, "bad_request", "Mode must be \"code\" or \"research\".")
		return
	}
	if req.Mode == ModeResearch && !s.cfg.ResearchEnabled {
		writeError(w, http.StatusBadRequest, "research_disabled", "Web research is not available on this server.")
		return
	}
	input, flags, err := guardrails.CheckInput(req.Content, s.cfg.MaxInputChars)
	if err != nil {
		s.guardrail(r.Context(), sess.id, ip, "input_rejected", err.Error())
		writeError(w, http.StatusBadRequest, "invalid_input", capitalise(err.Error())+".")
		return
	}
	// Credentials pasted into the chat are removed before the model, the
	// trace, or the database see them.
	input, secretKinds := guardrails.Redact(input)

	// 2. One turn at a time per session. Taken before the turn count is read,
	// so two requests arriving together cannot both pass the limit.
	if !sess.turn.TryLock() {
		writeError(w, http.StatusConflict, "turn_in_progress", "Wait for the current reply to finish.")
		return
	}
	defer sess.turn.Unlock()
	sess.busy.Store(true)
	defer sess.busy.Store(false)

	sess.mu.Lock()
	turns, contextTokens := sess.turns, sess.contextTokens
	sess.mu.Unlock()
	if turns >= s.cfg.MaxTurnsPerSession {
		writeError(w, http.StatusTooManyRequests, "session_limit",
			fmt.Sprintf("This session has used all %d turns. Start a new session to continue.", s.cfg.MaxTurnsPerSession))
		return
	}
	if s.cfg.MaxContextTokens > 0 && contextTokens > s.cfg.MaxContextTokens {
		writeError(w, http.StatusTooManyRequests, "context_full",
			"This conversation has grown too long. Reset the session to continue.")
		return
	}

	// 3. Global limits. Checked before the per-visitor limiters so that a
	// request refused here does not use up the visitor's allowance.
	if s.budget.Exhausted() {
		s.guardrail(r.Context(), sess.id, ip, "budget_exhausted", "")
		writeError(w, http.StatusServiceUnavailable, "budget_exhausted",
			"The demo has used its budget for today. It resets at midnight UTC; the guide and the recorded runs still work.")
		return
	}
	select {
	case s.slots <- struct{}{}:
		defer func() { <-s.slots }()
	default:
		writeError(w, http.StatusServiceUnavailable, "busy", "The demo is busy. Try again in a few seconds.")
		return
	}

	// 4. Per-visitor limits.
	if ok, retry := s.perMinute.Allow(ip); !ok {
		s.guardrail(r.Context(), sess.id, ip, "rate_limit_minute", "")
		writeLimited(w, "rate_limited", "You're sending messages quickly. Wait a moment and try again.", retry)
		return
	}
	if ok, retry := s.perDay.Allow(ip); !ok {
		s.guardrail(r.Context(), sess.id, ip, "rate_limit_day", "")
		writeLimited(w, "rate_limited", "You've reached today's message limit for this demo.", retry)
		return
	}
	if req.Mode == ModeResearch {
		if ok, retry := s.researchPerDay.Allow(ip); !ok {
			s.guardrail(r.Context(), sess.id, ip, "rate_limit_research", "")
			writeLimited(w, "rate_limited", "You've reached today's limit for web research. Code mode still works.", retry)
			return
		}
	}

	// 5. Stream the turn. If the browser stops reading, a write times out,
	// the turn is cancelled, and its slot is freed: a stalled client cannot
	// hold a place that other visitors are waiting for.
	ctx, cancel := context.WithTimeout(r.Context(), s.cfg.TurnTimeout)
	defer cancel()
	stream := newSSE(w, cancel)
	defer stream.close()

	turnIndex := turns + 1
	stream.send("turn_start", map[string]any{"turn": turnIndex, "mode": req.Mode, "model": s.cfg.Model})

	var trace []agent.Event
	emit := func(ev agent.Event) {
		if ev.Tool != nil && len(ev.Tool.Output) > maxTraceOutput {
			tool := *ev.Tool
			tool.Output, _ = guardrails.Truncate(tool.Output, maxTraceOutput)
			ev.Tool = &tool
		}
		stream.send(ev.Type, ev)
		if ev.Type != agent.EventTextDelta {
			trace = append(trace, ev)
		}
		// Show a file change as it happens, not at the end of the turn.
		// Only the edited file is sent; the full listing follows once, in
		// turn_end.
		if ev.Type == agent.EventToolResult && ev.Tool.Name == "edit_file" && !ev.Tool.IsError {
			var in struct {
				Path string `json:"path"`
			}
			if json.Unmarshal(ev.Tool.Input, &in) == nil {
				if clean, err := workspace.Clean(in.Path); err == nil {
					if data, err := sess.fs.ReadFile(clean); err == nil {
						stream.send("file", fileView{Path: clean, Content: string(data)})
					}
				}
			}
		}
	}
	for _, flag := range flags {
		emit(agent.Event{Type: agent.EventGuardrail, Guardrail: &agent.Guardrail{
			Kind: flag, Detail: "noted for review; the request still runs because the agent can only touch this sandbox"}})
	}
	if len(secretKinds) > 0 {
		flags = append(flags, "secret_redacted_input")
		emit(agent.Event{Type: agent.EventGuardrail, Guardrail: &agent.Guardrail{
			Kind: "secret_redacted", Detail: "in your message: " + strings.Join(secretKinds, ", ")}})
	}

	sess.mu.Lock()
	history := sess.conv
	sess.mu.Unlock()

	start := time.Now()
	conv, res, turnErr := ag.Turn(ctx, history, sess.fs, input, emit)
	latency := time.Since(start)

	// 6. Account for it, whether or not it succeeded.
	s.budget.Add(res.Usage.CostUSD)
	view := turnView{Mode: req.Mode, Input: input, Reply: res.Text, Trace: trace, Usage: res.Usage, Sources: res.Sources}
	var errMsg string
	if turnErr != nil {
		errMsg = publicError(turnErr)
		view.Error = errMsg
		s.log.Error("turn failed", "session", sess.id, "err", turnErr)
		stream.send("error", map[string]any{"code": "turn_failed", "message": errMsg})
	}

	sess.mu.Lock()
	sess.conv = conv
	sess.turns++
	sess.transcript = append(sess.transcript, view)
	if res.Usage.InputTokens > 0 && res.Rounds > 0 {
		// Input tokens are summed across rounds; the per-round average is a
		// fair estimate of how large the context has become.
		sess.contextTokens = res.Usage.InputTokens / int64(res.Rounds)
	}
	sess.lastActive = time.Now().UTC()
	turnsUsed := sess.turns
	sess.mu.Unlock()

	stream.send("turn_end", map[string]any{
		"turn":        turnIndex,
		"turns_used":  turnsUsed,
		"turns_max":   s.cfg.MaxTurnsPerSession,
		"usage":       res.Usage,
		"rounds":      res.Rounds,
		"stop_reason": res.StopReason,
		"latency_ms":  latency.Milliseconds(),
		"files":       fileViews(sess.fs.Snapshot()),
	})

	// 7. Persist after the response.
	for _, g := range res.Guardrails {
		flags = append(flags, g.Kind)
	}
	s.persistSession(sess)
	turn := store.Turn{
		SessionID: sess.id, Index: turnIndex, Mode: req.Mode, Model: s.cfg.Model,
		Input: input, Reply: res.Text, StopReason: res.StopReason, Rounds: res.Rounds,
		InputTokens: res.Usage.InputTokens, OutputTokens: res.Usage.OutputTokens,
		WebSearches: res.Usage.WebSearches, CostUSD: res.Usage.CostUSD,
		LatencyMS: latency.Milliseconds(), Flags: dedupe(flags), Error: errMsg, Trace: trace,
	}
	s.async("save_turn", func(ctx context.Context) error { return s.store.SaveTurn(ctx, turn) })
	usage := store.Usage{Turns: 1, InputTokens: res.Usage.InputTokens, OutputTokens: res.Usage.OutputTokens,
		WebSearches: res.Usage.WebSearches, CostUSD: res.Usage.CostUSD}
	s.async("add_usage", func(ctx context.Context) error { return s.store.AddUsage(ctx, usage) })
	for _, g := range res.Guardrails {
		s.guardrail(ctx, sess.id, ip, g.Kind, g.Detail)
	}
	if turnErr == nil && req.Mode == ModeResearch && res.Usage.WebSearches > 0 && res.Text != "" {
		// Saved unpublished. Answers appear in the public library only
		// after a person has reviewed them and set is_public.
		answer := store.Research{SessionID: sess.id, Question: input, Answer: res.Text, Sources: res.Sources, Model: s.cfg.Model}
		s.async("save_research", func(ctx context.Context) error { return s.store.SaveResearch(ctx, answer) })
	}
}

// publicError turns an internal error into something safe and useful to
// show. Provider messages can include request details, so only the status
// is passed on; the full error is in the server log.
func publicError(err error) string {
	var apiErr *anthropic.Error
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "The turn took too long and was stopped."
	case errors.Is(err, context.Canceled):
		return "The request was cancelled."
	case errors.As(err, &apiErr):
		switch apiErr.StatusCode {
		case http.StatusTooManyRequests, 529:
			return "The model is overloaded right now. Try again in a moment."
		case http.StatusUnauthorized, http.StatusForbidden:
			return "The server's model credentials were rejected. The site owner needs to check the API key."
		default:
			return fmt.Sprintf("The model returned an error (status %d). Try again.", apiErr.StatusCode)
		}
	}
	return "The model could not be reached. Try again."
}

func capitalise(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func dedupe(in []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// ---- server-sent events ---------------------------------------------------

// sseWriteTimeout is how long one write to the browser may take. A healthy
// connection takes microseconds; this only fires when the other end has
// stopped reading.
const sseWriteTimeout = 10 * time.Second

type sse struct {
	mu     sync.Mutex
	w      http.ResponseWriter
	rc     *http.ResponseController
	closed bool
	onFail func() // called once when a write fails

	done chan struct{}
	wg   sync.WaitGroup
}

// newSSE starts an event stream and a heartbeat that keeps idle proxies from
// closing the connection while the model is thinking. onFail is called if
// the client can no longer be written to.
func newSSE(w http.ResponseWriter, onFail func()) *sse {
	h := w.Header()
	h.Set("Content-Type", "text/event-stream; charset=utf-8")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	s := &sse{w: w, rc: http.NewResponseController(w), onFail: onFail, done: make(chan struct{})}
	s.mu.Lock()
	s.writeLocked("")
	s.mu.Unlock()

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		t := time.NewTicker(15 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-s.done:
				return
			case <-t.C:
				s.mu.Lock()
				s.writeLocked(": keep-alive\n\n")
				s.mu.Unlock()
			}
		}
	}()
	return s
}

// writeLocked writes and flushes under a deadline. After close, or after a
// failed write, it does nothing: the response writer must not be touched
// once the handler has returned.
func (s *sse) writeLocked(payload string) {
	if s.closed {
		return
	}
	_ = s.rc.SetWriteDeadline(time.Now().Add(sseWriteTimeout))
	_, err := io.WriteString(s.w, payload)
	if err == nil {
		err = s.rc.Flush()
	}
	if err != nil {
		s.closed = true
		if s.onFail != nil {
			s.onFail()
		}
	}
}

func (s *sse) send(event string, data any) {
	payload, err := json.Marshal(data)
	if err != nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.writeLocked(fmt.Sprintf("event: %s\ndata: %s\n\n", event, payload))
}

// close stops the heartbeat and waits for it, so nothing writes to the
// response after the handler returns.
func (s *sse) close() {
	s.mu.Lock()
	s.closed = true
	// The deadline is set on the connection, which may be reused for the
	// next request; clear it so that request does not inherit an expired one.
	_ = s.rc.SetWriteDeadline(time.Time{})
	s.mu.Unlock()
	close(s.done)
	s.wg.Wait()
}
