package store

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/aurimas13/Code-Editing-Agent/internal/agent"
)

// These tests run the store and the migration against a real Postgres
// behind PostgREST, which is what a Supabase project is. They are skipped
// unless a target is configured:
//
//	SUPABASE_TEST_URL              a Supabase URL (for example from `supabase start`), or
//	POSTGREST_TEST_URL             a bare PostgREST URL
//	SUPABASE_TEST_SECRET_KEY       a key with the service_role role
//	SUPABASE_TEST_PUBLISHABLE_KEY  a key with the anon role
//
// The database must have supabase/migrations applied and be disposable:
// the tests insert rows.
func integrationTarget(t *testing.T) (baseURL, secret, publishable string) {
	t.Helper()
	secret, publishable = os.Getenv("SUPABASE_TEST_SECRET_KEY"), os.Getenv("SUPABASE_TEST_PUBLISHABLE_KEY")
	if u := os.Getenv("SUPABASE_TEST_URL"); u != "" && secret != "" {
		return u, secret, publishable
	}
	raw := os.Getenv("POSTGREST_TEST_URL")
	if raw == "" || secret == "" {
		t.Skip("no integration database configured")
	}
	// Supabase serves PostgREST under /rest/v1; a bare PostgREST serves at
	// the root. A small proxy makes the bare server look like Supabase.
	target, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	srv := httptest.NewServer(http.StripPrefix("/rest/v1", proxy))
	t.Cleanup(srv.Close)
	return srv.URL, secret, publishable
}

func TestIntegrationStoreRoundTrip(t *testing.T) {
	baseURL, secret, _ := integrationTarget(t)
	st := NewSupabase(baseURL, secret)
	ctx := context.Background()
	id := testUUID(t)

	now := time.Now().UTC().Truncate(time.Second)
	sess := Session{ID: id, TokenHash: "hash", IPHash: "ip", Model: "m", State: json.RawMessage(`{"files":{"a.txt":"one"}}`), CreatedAt: now, LastActiveAt: now}
	must(t, st.SaveSession(ctx, sess))
	sess.TurnCount, sess.State = 2, json.RawMessage(`{"files":{"a.txt":"two"}}`)
	must(t, st.SaveSession(ctx, sess)) // upsert, not a duplicate-key error

	got, err := st.LoadSession(ctx, id)
	must(t, err)
	if got == nil || got.TurnCount != 2 || !strings.Contains(string(got.State), "two") || !got.CreatedAt.Equal(now) {
		t.Fatalf("LoadSession = %+v", got)
	}
	if missing, err := st.LoadSession(ctx, testUUID(t)); err != nil || missing != nil {
		t.Errorf("missing session: %v, %v", missing, err)
	}

	turn := Turn{SessionID: id, Index: 1, Mode: "code", Model: "m", Input: "hi", Reply: "hello", StopReason: "end_turn",
		Rounds: 2, InputTokens: 100, OutputTokens: 20, CostUSD: 0.0002, LatencyMS: 1200,
		Flags: []string{"secret_redacted"}, Trace: []agent.Event{{Type: "tool_call", Tool: &agent.ToolEvent{ID: "t1", Name: "read_file"}}}}
	must(t, st.SaveTurn(ctx, turn))
	if err := st.SaveTurn(ctx, turn); err == nil {
		t.Error("saving the same turn twice should violate the unique (session_id, idx) constraint")
	}
	turn.Index, turn.Mode = 2, "shell"
	if err := st.SaveTurn(ctx, turn); err == nil {
		t.Error("an unknown mode should violate the check constraint")
	}
	turn.Mode, turn.Flags = "research", nil
	must(t, st.SaveTurn(ctx, turn)) // nil flags must be stored as an empty array

	before, err := st.UsageToday(ctx)
	must(t, err)
	must(t, st.AddUsage(ctx, Usage{Turns: 1, InputTokens: 100, OutputTokens: 20, WebSearches: 1, CostUSD: 0.25}))
	must(t, st.AddUsage(ctx, Usage{Turns: 2, InputTokens: 50, OutputTokens: 10, CostUSD: 0.5}))
	after, err := st.UsageToday(ctx)
	must(t, err)
	if after.Turns-before.Turns != 3 || after.InputTokens-before.InputTokens != 150 || after.WebSearches-before.WebSearches != 1 ||
		after.CostUSD-before.CostUSD < 0.7499 || after.CostUSD-before.CostUSD > 0.7501 {
		t.Errorf("usage did not accumulate: before %+v after %+v", before, after)
	}

	must(t, st.LogGuardrail(ctx, GuardrailEvent{IPHash: "ip", Kind: "rate_limit_minute"}))
	must(t, st.LogGuardrail(ctx, GuardrailEvent{SessionID: id, IPHash: "ip", Kind: "workspace_boundary", Detail: "path escapes"}))
	must(t, st.SaveEvalRun(ctx, EvalRun{Suite: "integration", Passed: 1, Total: 1, Report: json.RawMessage(`{"ok":true}`)}))

	// Research answers start private and appear in the library only after
	// someone sets is_public, which the store itself has no method to do.
	question := "integration question " + id
	must(t, st.SaveResearch(ctx, Research{SessionID: id, Question: question, Answer: "an answer", Model: "m",
		Sources: []agent.Source{{URL: "https://go.dev", Title: "Go", Cited: true}}}))
	if listed(t, st, question) != nil {
		t.Fatal("an unpublished answer is in the public library")
	}
	status, _ := rawRequest(t, http.MethodPatch, baseURL+"/rest/v1/research_answers?question=eq."+url.QueryEscape(question), secret, `{"is_public":true}`)
	if status >= 300 {
		t.Fatalf("publishing failed: %d", status)
	}
	published := listed(t, st, question)
	if published == nil || published.Answer != "an answer" || len(published.Sources) != 1 || !published.Sources[0].Cited || published.SessionID != "" || published.CreatedAt.IsZero() {
		t.Errorf("published answer = %+v", published)
	}
}

// The publishable key ships to browsers in any Supabase project. This is
// what it must and must not be able to do.
func TestIntegrationPublicKeyAccess(t *testing.T) {
	baseURL, secret, publishable := integrationTarget(t)
	if publishable == "" {
		t.Skip("no publishable key configured")
	}
	st := NewSupabase(baseURL, secret)
	ctx := context.Background()
	id := testUUID(t)
	must(t, st.SaveSession(ctx, Session{ID: id, TokenHash: "h", IPHash: "i", Model: "m", State: json.RawMessage(`{}`), CreatedAt: time.Now(), LastActiveAt: time.Now()}))
	must(t, st.SaveResearch(ctx, Research{SessionID: id, Question: "private " + id, Answer: "a", Model: "m"}))
	must(t, st.SaveResearch(ctx, Research{SessionID: id, Question: "public " + id, Answer: "a", Model: "m", IsPublic: true}))
	must(t, st.SaveEvalRun(ctx, EvalRun{Suite: "public-key", Passed: 1, Total: 1, Report: json.RawMessage(`{}`)}))

	rest := baseURL + "/rest/v1"
	denied := []struct{ name, method, path, body string }{
		{"read sessions", "GET", "/sessions?select=id", ""},
		{"read turns", "GET", "/turns?select=id", ""},
		{"read usage", "GET", "/usage_daily?select=day", ""},
		{"read guardrail events", "GET", "/guardrail_events?select=id", ""},
		{"read session ids of research answers", "GET", "/research_answers?select=session_id", ""},
		{"read every research column", "GET", "/research_answers?select=*", ""},
		{"insert a research answer", "POST", "/research_answers", `{"question":"q","answer":"a","model":"m","is_public":true}`},
		{"publish an answer", "PATCH", "/research_answers?is_public=eq.false", `{"is_public":true}`},
		{"delete answers", "DELETE", "/research_answers?is_public=eq.true", ""},
		{"insert an eval run", "POST", "/eval_runs", `{"suite":"forged","passed":99,"failed":0,"total":99,"report":{}}`},
		{"add usage", "POST", "/rpc/add_usage", `{"p_turns":1,"p_input_tokens":1,"p_output_tokens":1,"p_web_searches":0,"p_cost_usd":-100}`},
		{"purge data", "POST", "/rpc/purge_expired", `{}`},
	}
	for _, d := range denied {
		status, body := rawRequest(t, d.method, rest+d.path, publishable, d.body)
		if status != http.StatusUnauthorized && status != http.StatusForbidden && status != http.StatusNotFound {
			t.Errorf("public key could %s: status %d, body %s", d.name, status, body)
		}
	}

	status, body := rawRequest(t, "GET", rest+"/research_answers?select=id,question,answer,sources,model,created_at", publishable, "")
	if status != http.StatusOK || !strings.Contains(body, "public "+id) || strings.Contains(body, "private "+id) {
		t.Errorf("public key reading published answers: status %d, body %s", status, body)
	}
	status, body = rawRequest(t, "GET", rest+"/eval_runs?select=suite&suite=eq.public-key", publishable, "")
	if status != http.StatusOK || !strings.Contains(body, "public-key") {
		t.Errorf("public key reading eval runs: status %d, body %s", status, body)
	}
}

func listed(t *testing.T, st *Supabase, question string) *Research {
	t.Helper()
	answers, err := st.ListResearch(context.Background(), 100)
	must(t, err)
	for i := range answers {
		if answers[i].Question == question {
			return &answers[i]
		}
	}
	return nil
}

func rawRequest(t *testing.T, method, target, key, body string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(method, target, bytes.NewBufferString(body))
	must(t, err)
	req.Header.Set("apikey", key)
	if strings.HasPrefix(key, "eyJ") {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	must(t, err)
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(raw)
}

func testUUID(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile("/proc/sys/kernel/random/uuid")
	if err == nil {
		return strings.TrimSpace(string(raw))
	}
	// Portable fallback: time-based, unique enough for a test run.
	n := time.Now().UnixNano()
	const hex = "0123456789abcdef"
	b := []byte("00000000-0000-4000-8000-000000000000")
	for i := len(b) - 1; i >= 0 && n > 0; i-- {
		if b[i] == '-' {
			continue
		}
		b[i] = hex[n&0xf]
		n >>= 4
	}
	return string(b)
}
