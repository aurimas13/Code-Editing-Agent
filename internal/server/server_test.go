package server

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/anthropics/anthropic-sdk-go/option"

	"github.com/aurimas13/Code-Editing-Agent/internal/llm"
	"github.com/aurimas13/Code-Editing-Agent/internal/llm/fakellm"
	"github.com/aurimas13/Code-Editing-Agent/internal/store"
)

type harness struct {
	t     *testing.T
	api   *Server
	http  *httptest.Server
	store *store.Memory
	fake  *fakellm.Server
}

func newHarness(t *testing.T, mutate func(*Config)) *harness {
	return newHarnessWith(t, fakellm.DemoBrain, mutate)
}

func newHarnessWith(t *testing.T, responder fakellm.Responder, mutate func(*Config)) *harness {
	t.Helper()
	fake := fakellm.New(responder)
	baseURL, closeFake, err := fake.Listen()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(closeFake)
	client := llm.NewAnthropic(option.WithBaseURL(baseURL), option.WithAPIKey("test"), option.WithMaxRetries(0))

	cfg := DefaultConfig()
	cfg.TrustedProxyHops = 0
	cfg.IPHashKey = "test-key"
	if mutate != nil {
		mutate(&cfg)
	}
	mem := store.NewMemory()
	api := New(cfg, client, mem, false, slog.New(slog.NewTextHandler(io.Discard, nil)))
	srv := httptest.NewServer(api.Handler())
	t.Cleanup(srv.Close)
	return &harness{t: t, api: api, http: srv, store: mem, fake: fake}
}

type sessionInfo struct {
	ID         string     `json:"id"`
	Token      string     `json:"token"`
	Files      []fileView `json:"files"`
	Transcript []turnView `json:"transcript"`
	TurnsUsed  int        `json:"turns_used"`
}

func (h *harness) do(method, path, token string, body any, headers ...string) *http.Response {
	h.t.Helper()
	var reader io.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		reader = bytes.NewReader(raw)
	}
	req, _ := http.NewRequest(method, h.http.URL+path, reader)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	h.t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func (h *harness) newSession() sessionInfo {
	h.t.Helper()
	resp := h.do("POST", "/api/sessions", "", nil)
	if resp.StatusCode != http.StatusCreated {
		h.t.Fatalf("create session: %s", resp.Status)
	}
	var s sessionInfo
	decode(h.t, resp, &s)
	return s
}

type sseEvent struct {
	Name string
	Data map[string]any
}

// send posts a message and returns the events of the reply stream.
func (h *harness) send(s sessionInfo, mode, content string) (int, []sseEvent, map[string]any) {
	h.t.Helper()
	resp := h.do("POST", "/api/sessions/"+s.ID+"/messages", s.Token, map[string]string{"content": content, "mode": mode})
	if !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		var body map[string]any
		decode(h.t, resp, &body)
		return resp.StatusCode, nil, body
	}
	var events []sseEvent
	var name string
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64<<10), 4<<20)
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "event: "):
			name = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			var data map[string]any
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &data); err != nil {
				h.t.Fatalf("bad event data: %v", err)
			}
			events = append(events, sseEvent{name, data})
		}
	}
	return resp.StatusCode, events, nil
}

func decode(t *testing.T, resp *http.Response, v any) {
	t.Helper()
	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		t.Fatalf("decode %s: %v", resp.Status, err)
	}
}

func names(events []sseEvent) string {
	var out []string
	for _, e := range events {
		if e.Name != "text_delta" {
			out = append(out, e.Name)
		}
	}
	return strings.Join(out, " ")
}

func errorCode(body map[string]any) string {
	e, _ := body["error"].(map[string]any)
	code, _ := e["code"].(string)
	return code
}

func fileContent(files []fileView, path string) (string, bool) {
	for _, f := range files {
		if f.Path == path {
			return f.Content, true
		}
	}
	return "", false
}

// ---- tests ----------------------------------------------------------------

func TestFullTurnOverHTTP(t *testing.T) {
	h := newHarness(t, nil)
	s := h.newSession()
	if _, ok := fileContent(s.Files, "main.go"); !ok || len(s.Files) != 5 {
		t.Fatalf("workspace not seeded: %d files", len(s.Files))
	}

	status, events, _ := h.send(s, "code", "Find and fix the bug in greet.js")
	if status != http.StatusOK {
		t.Fatalf("status %d", status)
	}
	want := "turn_start model_start usage tool_call tool_result model_start usage tool_call tool_result file model_start usage text done turn_end"
	if got := names(events); got != want {
		t.Errorf("event stream:\n got %s\nwant %s", got, want)
	}

	// The workspace the browser gets back has the fix in it.
	var after sessionInfo
	decode(t, h.do("GET", "/api/sessions/"+s.ID, s.Token, nil), &after)
	greet, _ := fileContent(after.Files, "greet.js")
	if !strings.Contains(greet, "+ name +") || strings.Contains(greet, "nmae") {
		t.Errorf("greet.js was not fixed:\n%s", greet)
	}
	if after.TurnsUsed != 1 || len(after.Transcript) != 1 || !strings.Contains(after.Transcript[0].Reply, "Fixed") {
		t.Errorf("transcript not recorded: %+v", after)
	}

	// And everything was persisted: session, trace, usage.
	h.api.Wait()
	if len(h.store.Turns) != 1 {
		t.Fatalf("saved %d turns", len(h.store.Turns))
	}
	turn := h.store.Turns[0]
	if turn.SessionID != s.ID || turn.Rounds != 3 || turn.InputTokens == 0 || turn.CostUSD <= 0 || len(turn.Trace) == 0 {
		t.Errorf("saved turn is incomplete: %+v", turn)
	}
	for _, ev := range turn.Trace {
		if ev.Type == "text_delta" {
			t.Fatal("streaming deltas were written to the trace")
		}
	}
	usage, _ := h.store.UsageToday(context.Background())
	if usage.Turns != 1 || usage.CostUSD <= 0 {
		t.Errorf("usage not recorded: %+v", usage)
	}
	saved, _ := h.store.LoadSession(context.Background(), s.ID)
	if saved == nil || strings.Contains(string(saved.State), s.Token) || saved.TokenHash == s.Token {
		t.Error("session missing from the store, or the bearer token was stored")
	}
}

func TestSessionsAreIsolated(t *testing.T) {
	h := newHarness(t, nil)
	a, b := h.newSession(), h.newSession()
	h.send(a, "code", "Find and fix the bug in greet.js")

	var bAfter sessionInfo
	decode(t, h.do("GET", "/api/sessions/"+b.ID, b.Token, nil), &bAfter)
	if greet, _ := fileContent(bAfter.Files, "greet.js"); !strings.Contains(greet, "nmae") {
		t.Error("one visitor's edit changed another visitor's workspace")
	}
	// One session's token does not open another session.
	if resp := h.do("GET", "/api/sessions/"+b.ID, a.Token, nil); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("cross-session access returned %s", resp.Status)
	}
	if resp := h.do("GET", "/api/sessions/"+b.ID, "", nil); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("missing token returned %s", resp.Status)
	}
	if resp := h.do("GET", "/api/sessions/00000000-0000-4000-8000-000000000000", a.Token, nil); resp.StatusCode != http.StatusNotFound {
		t.Errorf("unknown session returned %s", resp.Status)
	}
}

func TestSandboxHoldsOverHTTP(t *testing.T) {
	h := newHarness(t, nil)
	s := h.newSession()
	_, events, _ := h.send(s, "code", "read ../../etc/passwd")
	var sawBoundary, sawError bool
	for _, e := range events {
		if e.Name == "guardrail" {
			g, _ := e.Data["guardrail"].(map[string]any)
			sawBoundary = sawBoundary || g["kind"] == "workspace_boundary"
		}
		if e.Name == "tool_result" {
			tool, _ := e.Data["tool"].(map[string]any)
			sawError = sawError || tool["is_error"] == true
		}
	}
	if !sawBoundary || !sawError {
		t.Errorf("traversal was not refused and reported: %s", names(events))
	}
	h.api.Wait()
	found := false
	for _, g := range h.store.Guardrails {
		found = found || g.Kind == "workspace_boundary"
	}
	if !found {
		t.Error("the boundary violation was not logged for review")
	}
}

func TestInputValidation(t *testing.T) {
	h := newHarness(t, func(c *Config) { c.MaxInputChars = 50 })
	s := h.newSession()
	cases := []struct{ mode, content, code string }{
		{"code", "   ", "invalid_input"},
		{"code", strings.Repeat("a", 51), "invalid_input"},
		{"shell", "hello", "bad_request"},
	}
	for _, c := range cases {
		status, _, body := h.send(s, c.mode, c.content)
		if status != http.StatusBadRequest || errorCode(body) != c.code {
			t.Errorf("%q/%q: status %d code %q, want 400 %q", c.mode, c.content, status, errorCode(body), c.code)
		}
	}
	// Rejected requests must not reach the model or use up a turn.
	if n := len(h.fake.Requests()); n != 0 {
		t.Errorf("%d model calls were made for rejected input", n)
	}
	var after sessionInfo
	decode(t, h.do("GET", "/api/sessions/"+s.ID, s.Token, nil), &after)
	if after.TurnsUsed != 0 {
		t.Errorf("rejected input used %d turns", after.TurnsUsed)
	}
}

func TestCredentialInMessageNeverReachesModelOrStore(t *testing.T) {
	h := newHarness(t, nil)
	s := h.newSession()
	secret := "sk-ant-api03-" + strings.Repeat("Qq7", 12)
	_, events, _ := h.send(s, "code", "my key is "+secret+" please remember it")

	requests, _ := json.Marshal(h.fake.Requests())
	if strings.Contains(string(requests), secret) {
		t.Error("the credential was sent to the model")
	}
	if !strings.Contains(names(events), "guardrail") {
		t.Error("the visitor was not told their message was redacted")
	}
	h.api.Wait()
	saved, _ := json.Marshal(h.store.Turns)
	state, _ := h.store.LoadSession(context.Background(), s.ID)
	if strings.Contains(string(saved), secret) || strings.Contains(string(state.State), secret) {
		t.Error("the credential was written to the store")
	}
}

func TestSessionTurnLimit(t *testing.T) {
	h := newHarness(t, func(c *Config) { c.MaxTurnsPerSession = 2 })
	s := h.newSession()
	for i := 0; i < 2; i++ {
		if status, _, _ := h.send(s, "code", "What do you see in this directory?"); status != http.StatusOK {
			t.Fatalf("turn %d: status %d", i+1, status)
		}
	}
	status, _, body := h.send(s, "code", "one more")
	if status != http.StatusTooManyRequests || errorCode(body) != "session_limit" {
		t.Errorf("third turn: status %d code %q", status, errorCode(body))
	}
	// Reset clears the conversation but does not hand back the turns.
	var reset sessionInfo
	decode(t, h.do("POST", "/api/sessions/"+s.ID+"/reset", s.Token, nil), &reset)
	if reset.TurnsUsed != 2 || len(reset.Transcript) != 0 {
		t.Errorf("after reset: %+v", reset)
	}
}

func TestPerVisitorRateLimit(t *testing.T) {
	h := newHarness(t, func(c *Config) { c.TurnsPerMinute = 2 })
	s := h.newSession()
	for i := 0; i < 2; i++ {
		h.send(s, "code", "What do you see in this directory?")
	}
	resp := h.do("POST", "/api/sessions/"+s.ID+"/messages", s.Token, map[string]string{"content": "again"})
	var body map[string]any
	decode(t, resp, &body)
	if resp.StatusCode != http.StatusTooManyRequests || errorCode(body) != "rate_limited" || resp.Header.Get("Retry-After") == "" {
		t.Errorf("status %s code %q retry-after %q", resp.Status, errorCode(body), resp.Header.Get("Retry-After"))
	}
	// A second session from the same address shares the limit.
	other := h.newSession()
	if status, _, _ := h.send(other, "code", "hello"); status != http.StatusTooManyRequests {
		t.Errorf("a new session bypassed the per-visitor limit: status %d", status)
	}
}

func TestSessionCreationLimit(t *testing.T) {
	h := newHarness(t, func(c *Config) { c.SessionsPerHour = 2 })
	h.newSession()
	h.newSession()
	if resp := h.do("POST", "/api/sessions", "", nil); resp.StatusCode != http.StatusTooManyRequests {
		t.Errorf("third session: %s", resp.Status)
	}
}

func TestDailyBudgetStopsModelCalls(t *testing.T) {
	h := newHarness(t, func(c *Config) { c.DailyBudgetUSD = 0.000001 })
	s := h.newSession()
	if status, _, _ := h.send(s, "code", "What do you see in this directory?"); status != http.StatusOK {
		t.Fatalf("first turn: status %d", status)
	}
	calls := len(h.fake.Requests())
	status, _, body := h.send(s, "code", "again")
	if status != http.StatusServiceUnavailable || errorCode(body) != "budget_exhausted" {
		t.Errorf("status %d code %q", status, errorCode(body))
	}
	if len(h.fake.Requests()) != calls {
		t.Error("the model was called after the budget ran out")
	}
	var cfg struct {
		Budget struct {
			Exhausted bool `json:"exhausted"`
		} `json:"budget"`
	}
	decode(t, h.do("GET", "/api/config", "", nil), &cfg)
	if !cfg.Budget.Exhausted {
		t.Error("/api/config does not report the exhausted budget")
	}
}

func TestBudgetResumesFromStoreAfterRestart(t *testing.T) {
	mem := store.NewMemory()
	_ = mem.AddUsage(context.Background(), store.Usage{Turns: 3, CostUSD: 5})
	cfg := DefaultConfig()
	cfg.DailyBudgetUSD = 3
	api := New(cfg, nil, mem, false, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if !api.budget.Exhausted() {
		t.Error("a restart reset the daily budget")
	}
}

func TestResearchModeGating(t *testing.T) {
	h := newHarness(t, func(c *Config) { c.ResearchEnabled = false })
	s := h.newSession()
	status, _, body := h.send(s, "research", "What is new in Go?")
	if status != http.StatusBadRequest || errorCode(body) != "research_disabled" {
		t.Errorf("status %d code %q", status, errorCode(body))
	}
}

func TestSessionSurvivesRestart(t *testing.T) {
	h := newHarness(t, nil)
	s := h.newSession()
	h.send(s, "code", "Find and fix the bug in greet.js")
	h.api.Wait()

	// A new server process with the same store and no in-memory sessions.
	fake := fakellm.New(fakellm.DemoBrain)
	baseURL, closeFake, _ := fake.Listen()
	defer closeFake()
	client := llm.NewAnthropic(option.WithBaseURL(baseURL), option.WithAPIKey("test"), option.WithMaxRetries(0))
	cfg := DefaultConfig()
	cfg.TrustedProxyHops = 0
	restarted := New(cfg, client, h.store, false, slog.New(slog.NewTextHandler(io.Discard, nil)))
	srv := httptest.NewServer(restarted.Handler())
	defer srv.Close()
	h2 := &harness{t: t, api: restarted, http: srv, store: h.store, fake: fake}

	var resumed sessionInfo
	resp := h2.do("GET", "/api/sessions/"+s.ID, s.Token, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("resume: %s", resp.Status)
	}
	decode(t, resp, &resumed)
	greet, _ := fileContent(resumed.Files, "greet.js")
	if resumed.TurnsUsed != 1 || len(resumed.Transcript) != 1 || strings.Contains(greet, "nmae") {
		t.Errorf("session was not restored: turns=%d transcript=%d", resumed.TurnsUsed, len(resumed.Transcript))
	}
	// The restored conversation is accepted by the model for a new turn.
	if status, events, _ := h2.send(s, "code", "What do you see in this directory?"); status != http.StatusOK || !strings.Contains(names(events), "turn_end") {
		t.Errorf("turn after restore failed: %d %s", status, names(events))
	}
	if n := len(fake.Requests()[0].Messages); n < 7 {
		t.Errorf("restored turn sent only %d messages; earlier history was lost", n)
	}
	// The wrong token still does not work after a restore.
	if resp := h2.do("GET", "/api/sessions/"+s.ID, "wrong", nil); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("wrong token after restore: %s", resp.Status)
	}
}

func TestCORS(t *testing.T) {
	h := newHarness(t, func(c *Config) {
		c.AllowedOrigins = []string{"https://code.aurimas.io", "https://code-editing-agent-*.vercel.app"}
	})
	cases := map[string]bool{
		"https://code.aurimas.io":                        true,
		"https://code-editing-agent-git-main.vercel.app": true,
		"https://evil.example":                           false,
		"https://code.aurimas.io.evil.example":           false,
		"https://code-editing-agent-x.evil.vercel.app":   false,
		"https://code-editing-agent-.vercel.app":         false,
		"http://code.aurimas.io":                         false,
	}
	for origin, allowed := range cases {
		resp := h.do("OPTIONS", "/api/sessions", "", nil, "Origin", origin, "Access-Control-Request-Method", "POST")
		got := resp.Header.Get("Access-Control-Allow-Origin")
		if allowed && got != origin || !allowed && got != "" {
			t.Errorf("origin %s: Access-Control-Allow-Origin = %q", origin, got)
		}
	}
}

func TestClientIP(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "10.0.0.9:4444"
	req.Header.Set("X-Forwarded-For", "6.6.6.6, 203.0.113.7")
	req.Header.Set("X-Real-IP", "198.51.100.4")

	// The default trusts no header at all.
	if got := clientIP(req, "", 0); got != "10.0.0.9" {
		t.Errorf("default: %s, want the socket address", got)
	}
	if got := DefaultConfig(); got.TrustedProxyHops != 0 || got.ClientIPHeader != "" {
		t.Errorf("the default configuration trusts a client-controlled header: %+v", got)
	}
	if got := clientIP(req, "", 1); got != "203.0.113.7" {
		t.Errorf("one trusted hop: %s (a forged left-hand entry must be ignored)", got)
	}
	if got := clientIP(req, "", 5); got != "10.0.0.9" {
		t.Errorf("fewer entries than hops: %s", got)
	}
	if got := clientIP(req, "X-Real-IP", 0); got != "198.51.100.4" {
		t.Errorf("platform header: %s", got)
	}
	// IPv6 callers are limited per /64, not per address.
	a := httptest.NewRequest("GET", "/", nil)
	a.RemoteAddr = "[2001:db8:1:2:aaaa::1]:1"
	b := httptest.NewRequest("GET", "/", nil)
	b.RemoteAddr = "[2001:db8:1:2:bbbb::2]:1"
	c := httptest.NewRequest("GET", "/", nil)
	c.RemoteAddr = "[2001:db8:1:3::1]:1"
	if clientIP(a, "", 0) != clientIP(b, "", 0) || clientIP(a, "", 0) == clientIP(c, "", 0) {
		t.Errorf("IPv6 keys: %s %s %s", clientIP(a, "", 0), clientIP(b, "", 0), clientIP(c, "", 0))
	}
}

func TestForgedForwardedHeaderDoesNotDodgeLimits(t *testing.T) {
	h := newHarness(t, func(c *Config) { c.SessionsPerHour = 2 }) // default: no header trusted
	created := 0
	for i := 0; i < 6; i++ {
		resp := h.do("POST", "/api/sessions", "", nil, "X-Forwarded-For", fmt.Sprintf("203.0.113.%d", i), "X-Real-IP", fmt.Sprintf("198.51.100.%d", i))
		if resp.StatusCode == http.StatusCreated {
			created++
		}
	}
	if created != 2 {
		t.Errorf("created %d sessions by rotating headers, limit is 2", created)
	}
}

func TestRequestThrottleCoversRejectedRequests(t *testing.T) {
	h := newHarness(t, func(c *Config) { c.RequestsPerMinute = 10 })
	s := h.newSession()
	limited := 0
	for i := 0; i < 30; i++ {
		status, _, body := h.send(s, "code", "   ") // rejected: blank
		if status == http.StatusTooManyRequests && errorCode(body) == "rate_limited" {
			limited++
		}
	}
	if limited < 20 {
		t.Errorf("only %d of 30 rejected requests were throttled", limited)
	}
	h.api.Wait()
	if n := len(h.store.Guardrails); n > 10 {
		t.Errorf("%d guardrail rows written for a burst of rejected requests", n)
	}
}

func TestBogusSessionIDsNeverReachTheStore(t *testing.T) {
	h := newHarness(t, nil)
	counting := &countingStore{Store: h.api.store}
	h.api.sessions.store = counting
	for _, id := range []string{"x", "not-a-uuid", "1234", "../../etc/passwd", "AAAAAAAA-AAAA-4AAA-8AAA-AAAAAAAAAAAA"} {
		resp := h.do("GET", "/api/sessions/"+url.PathEscape(id), "token", nil)
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("id %q: %s", id, resp.Status)
		}
	}
	if counting.loads != 0 {
		t.Errorf("%d database lookups for malformed session ids", counting.loads)
	}
	// A well-formed but wrong token must not leave the session in memory.
	s := h.newSession()
	h.api.Wait()
	h.api.sessions.mu.Lock()
	delete(h.api.sessions.byID, s.ID)
	h.api.sessions.mu.Unlock()
	if resp := h.do("GET", "/api/sessions/"+s.ID, "wrong-token", nil); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong token: %s", resp.Status)
	}
	if h.api.sessions.count() != 0 {
		t.Error("a session was loaded into memory for a caller without its token")
	}
}

type countingStore struct {
	store.Store
	loads int
}

func (c *countingStore) LoadSession(ctx context.Context, id string) (*store.Session, error) {
	c.loads++
	return c.Store.LoadSession(ctx, id)
}

// stalledWriter imitates a browser that has stopped reading: writes fail
// once the deadline passes. It also fails the test if anything writes after
// the stream was closed, which on a real connection can crash the process.
type stalledWriter struct {
	t        *testing.T
	header   http.Header
	stalled  bool
	finished bool
	writes   int
}

func (w *stalledWriter) Header() http.Header { return w.header }
func (w *stalledWriter) WriteHeader(int)     {}
func (w *stalledWriter) Flush()              {}
func (w *stalledWriter) SetWriteDeadline(time.Time) error {
	return nil
}
func (w *stalledWriter) Write(p []byte) (int, error) {
	if w.finished {
		w.t.Error("write after the stream was closed")
	}
	w.writes++
	if w.stalled {
		return 0, os.ErrDeadlineExceeded
	}
	return len(p), nil
}

func TestStreamStopsWhenTheClientStopsReading(t *testing.T) {
	w := &stalledWriter{t: t, header: http.Header{}}
	failed := 0
	stream := newSSE(w, func() { failed++ })
	stream.send("text", map[string]string{"text": "hello"})
	if failed != 0 {
		t.Fatal("a healthy write was reported as a failure")
	}
	w.stalled = true
	stream.send("text", map[string]string{"text": "more"})
	if failed != 1 {
		t.Fatalf("the turn was cancelled %d times after a stalled write, want once", failed)
	}
	before := w.writes
	for i := 0; i < 5; i++ {
		stream.send("text", map[string]string{"text": "ignored"})
	}
	if w.writes != before || failed != 1 {
		t.Errorf("kept writing to a dead connection: %d extra writes", w.writes-before)
	}
	stream.close()
	w.finished = true
	time.Sleep(20 * time.Millisecond) // the heartbeat goroutine has exited; nothing may write now
}

func TestSecurityHeadersAndHealth(t *testing.T) {
	h := newHarness(t, nil)
	resp := h.do("GET", "/healthz", "", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("healthz: %s", resp.Status)
	}
	for header, want := range map[string]string{
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":        "DENY",
		"Cache-Control":          "no-store",
	} {
		if got := resp.Header.Get(header); got != want {
			t.Errorf("%s = %q, want %q", header, got, want)
		}
	}
}

func TestResearchLibraryShowsOnlyPublishedAnswers(t *testing.T) {
	h := newHarness(t, nil)
	ctx := context.Background()
	_ = h.store.SaveResearch(ctx, store.Research{SessionID: "s1", Question: "private q", Answer: "a", Model: "m"})
	_ = h.store.SaveResearch(ctx, store.Research{SessionID: "s2", Question: "public q", Answer: "a", Model: "m", IsPublic: true})
	raw, _ := io.ReadAll(h.do("GET", "/api/research", "", nil).Body)
	body := string(raw)
	if !strings.Contains(body, "public q") || strings.Contains(body, "private q") {
		t.Errorf("library = %s", body)
	}
	if strings.Contains(body, "s2") {
		t.Error("the library exposes session ids")
	}
}

func TestResearchAnswerIsSavedUnpublished(t *testing.T) {
	searching := fakellm.Response{Blocks: []fakellm.Block{
		{Type: "server_tool_use", ID: "srvtoolu_1", Name: "web_search", Input: json.RawMessage(`{"query":"go release"}`)},
		{Type: "web_search_tool_result", ToolUseID: "srvtoolu_1", Results: []fakellm.SearchResult{{URL: "https://go.dev/doc/devel/release", Title: "Release History"}}},
		{Type: "text", Text: "See the release history.", Citations: []fakellm.Citation{{URL: "https://go.dev/doc/devel/release", Title: "Release History"}}},
	}}
	h := newHarnessWith(t, fakellm.Script(searching, fakellm.Text("No search needed for that.")), nil)
	s := h.newSession()

	status, events, _ := h.send(s, "research", "What is the latest Go release?")
	if status != http.StatusOK || !strings.Contains(names(events), "web_search") || !strings.Contains(names(events), "sources") {
		t.Fatalf("status %d, events %s", status, names(events))
	}
	// The web search tool is offered in research mode, and only there.
	tools, _ := json.Marshal(h.fake.Requests()[0].Tools)
	if !strings.Contains(string(tools), "web_search") {
		t.Error("research mode did not offer web search")
	}

	// A research turn that did not search is a chat reply, not a research answer.
	h.send(s, "research", "Thanks")
	h.api.Wait()
	if len(h.store.Researches) != 1 {
		t.Fatalf("saved %d research answers, want 1", len(h.store.Researches))
	}
	saved := h.store.Researches[0]
	if saved.IsPublic || saved.Question != "What is the latest Go release?" || len(saved.Sources) != 1 || !saved.Sources[0].Cited {
		t.Errorf("saved answer = %+v", saved)
	}
	// Nothing reaches the public library until a person publishes it.
	raw, _ := io.ReadAll(h.do("GET", "/api/research", "", nil).Body)
	if strings.Contains(string(raw), "latest Go release") {
		t.Error("an unreviewed answer is in the public library")
	}
	usage, _ := h.store.UsageToday(context.Background())
	if usage.WebSearches != 1 {
		t.Errorf("web searches recorded in usage: %d", usage.WebSearches)
	}
}
