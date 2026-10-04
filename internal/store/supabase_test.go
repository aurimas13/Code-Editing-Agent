package store

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aurimas13/Code-Editing-Agent/internal/agent"
)

// recorded is one request the fake PostgREST endpoint received.
type recorded struct {
	Method, Path, Query, Prefer, APIKey, Auth string
	Body                                      map[string]any
}

func fakePostgREST(t *testing.T, respond func(r *http.Request) (int, string)) (*Supabase, *[]recorded) {
	t.Helper()
	var calls []recorded
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		calls = append(calls, recorded{r.Method, r.URL.Path, r.URL.RawQuery, r.Header.Get("Prefer"),
			r.Header.Get("apikey"), r.Header.Get("Authorization"), body})
		status, out := http.StatusCreated, ""
		if respond != nil {
			status, out = respond(r)
		}
		w.WriteHeader(status)
		_, _ = io.WriteString(w, out)
	}))
	t.Cleanup(srv.Close)
	return NewSupabase(srv.URL+"/", "sb_secret_test"), &calls
}

func TestSupabaseRequests(t *testing.T) {
	st, calls := fakePostgREST(t, nil)
	ctx := context.Background()

	must(t, st.SaveSession(ctx, Session{ID: "abc", TokenHash: "h", IPHash: "i", Model: "m", State: json.RawMessage(`{"files":{}}`)}))
	must(t, st.SaveTurn(ctx, Turn{SessionID: "abc", Index: 1, Mode: "code", Trace: []agent.Event{{Type: "done"}}}))
	must(t, st.AddUsage(ctx, Usage{Turns: 1, InputTokens: 10, CostUSD: 0.5}))
	must(t, st.LogGuardrail(ctx, GuardrailEvent{IPHash: "i", Kind: "rate_limit_minute"}))
	must(t, st.SaveResearch(ctx, Research{Question: "q", Answer: "a", Sources: []agent.Source{{URL: "https://go.dev"}}}))

	c := *calls
	if len(c) != 5 {
		t.Fatalf("made %d requests", len(c))
	}
	for i, call := range c {
		// A new-style secret key travels in the apikey header only.
		if call.APIKey != "sb_secret_test" || call.Auth != "" {
			t.Errorf("request %d: apikey=%q authorization=%q", i, call.APIKey, call.Auth)
		}
		if call.Method != http.MethodPost {
			t.Errorf("request %d: method %s", i, call.Method)
		}
	}
	if c[0].Path != "/rest/v1/sessions" || c[0].Query != "on_conflict=id" || !strings.Contains(c[0].Prefer, "merge-duplicates") {
		t.Errorf("session upsert: %+v", c[0])
	}
	if c[1].Path != "/rest/v1/turns" || c[1].Body["idx"] != float64(1) || c[1].Body["flags"] == nil {
		t.Errorf("turn insert: %+v", c[1])
	}
	if c[2].Path != "/rest/v1/rpc/add_usage" || c[2].Body["p_cost_usd"] != 0.5 {
		t.Errorf("usage rpc: %+v", c[2])
	}
	// Empty session ids must be sent as null, not "", or the foreign key fails.
	if v, present := c[3].Body["session_id"]; !present || v != nil {
		t.Errorf("guardrail session_id = %v", v)
	}
	// NOT NULL columns must never be sent as JSON null.
	if _, isList := c[4].Body["sources"].([]any); !isList {
		t.Errorf("research sources = %v, want a list", c[4].Body["sources"])
	}
	if c[4].Body["is_public"] != false {
		t.Errorf("research answers must be saved unpublished: %+v", c[4].Body)
	}
}

func TestSupabaseLegacyJWTKeyIsAlsoSentAsBearer(t *testing.T) {
	st, calls := fakePostgREST(t, nil)
	st.key = "eyJhbGciOiJIUzI1NiJ9.eyJyb2xlIjoic2VydmljZV9yb2xlIn0.signature"
	must(t, st.LogGuardrail(context.Background(), GuardrailEvent{IPHash: "i", Kind: "k"}))
	call := (*calls)[0]
	if call.APIKey != st.key || call.Auth != "Bearer "+st.key {
		t.Errorf("apikey=%q authorization=%q", call.APIKey, call.Auth)
	}
}

func TestSupabaseNeverSendsNullForRequiredColumns(t *testing.T) {
	st, calls := fakePostgREST(t, nil)
	ctx := context.Background()
	must(t, st.SaveSession(ctx, Session{ID: "abc"}))
	must(t, st.SaveTurn(ctx, Turn{SessionID: "abc"}))
	must(t, st.SaveResearch(ctx, Research{Question: "q"}))
	c := *calls
	if _, ok := c[0].Body["state"].(map[string]any); !ok {
		t.Errorf("session state = %v, want an object", c[0].Body["state"])
	}
	for _, field := range []string{"flags", "trace"} {
		if _, ok := c[1].Body[field].([]any); !ok {
			t.Errorf("turn %s = %v, want a list", field, c[1].Body[field])
		}
	}
	if _, ok := c[2].Body["sources"].([]any); !ok {
		t.Errorf("research sources = %v, want a list", c[2].Body["sources"])
	}
}

func TestSupabaseReads(t *testing.T) {
	st, calls := fakePostgREST(t, func(r *http.Request) (int, string) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/sessions") && r.URL.Query().Get("id") == "eq.missing":
			return 200, `[]`
		case strings.HasSuffix(r.URL.Path, "/sessions"):
			return 200, `[{"id":"abc","token_hash":"h","turn_count":3,"state":{"files":{"a":"b"}},"last_active_at":"2026-10-04T10:00:00Z"}]`
		case strings.HasSuffix(r.URL.Path, "/usage_daily"):
			return 200, `[{"day":"2026-10-04","turns":7,"cost_usd":1.25}]`
		case strings.HasSuffix(r.URL.Path, "/research_answers"):
			return 200, `[{"id":"r1","question":"q","answer":"a","sources":[{"url":"https://go.dev","title":"Go"}],"model":"m","created_at":"2026-10-04T10:00:00Z"}]`
		}
		return 404, `{"message":"no route"}`
	})
	ctx := context.Background()

	if s, err := st.LoadSession(ctx, "missing"); err != nil || s != nil {
		t.Errorf("missing session: %v, %v", s, err)
	}
	s, err := st.LoadSession(ctx, "abc")
	if err != nil || s == nil || s.TurnCount != 3 || !strings.Contains(string(s.State), `"a"`) || s.LastActiveAt.IsZero() {
		t.Errorf("LoadSession = %+v, %v", s, err)
	}
	u, err := st.UsageToday(ctx)
	if err != nil || u.Turns != 7 || u.CostUSD != 1.25 {
		t.Errorf("UsageToday = %+v, %v", u, err)
	}
	answers, err := st.ListResearch(ctx, 10)
	if err != nil || len(answers) != 1 || answers[0].Sources[0].Title != "Go" {
		t.Errorf("ListResearch = %+v, %v", answers, err)
	}
	last := (*calls)[len(*calls)-1]
	if !strings.Contains(last.Query, "is_public=eq.true") || strings.Contains(last.Query, "session_id") {
		t.Errorf("research query must filter on is_public and not select session_id: %s", last.Query)
	}
}

func TestSupabaseErrorsAreReported(t *testing.T) {
	st, _ := fakePostgREST(t, func(*http.Request) (int, string) {
		return 401, `{"message":"Invalid API key"}`
	})
	err := st.SaveTurn(context.Background(), Turn{SessionID: "abc"})
	if err == nil || !strings.Contains(err.Error(), "401") || !strings.Contains(err.Error(), "Invalid API key") {
		t.Errorf("err = %v", err)
	}
	if strings.Contains(err.Error(), "sb_secret_test") {
		t.Error("the error message contains the secret key")
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
