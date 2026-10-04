package store

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/aurimas13/Code-Editing-Agent/internal/agent"
)

// Supabase stores data in a Supabase project through its REST API
// (PostgREST). It needs only net/http, so the server binary has no database
// driver and connects over HTTPS like any other API client.
//
// It authenticates with the project's secret key (sb_secret_..., or a legacy
// service_role key), which bypasses row level security. That key lives only in the server's environment. The browser
// never talks to Supabase directly, and the tables deny everything to the
// public roles except reading published research answers and eval runs
// (see supabase/migrations).
type Supabase struct {
	baseURL string
	key     string
	http    *http.Client
}

// NewSupabase returns a store for the project at projectURL
// (https://<ref>.supabase.co).
func NewSupabase(projectURL, secretKey string) *Supabase {
	return &Supabase{
		baseURL: strings.TrimRight(projectURL, "/") + "/rest/v1",
		key:     secretKey,
		http:    &http.Client{Timeout: 10 * time.Second},
	}
}

func (s *Supabase) Name() string { return "supabase" }

// do sends one request. out, if not nil, receives the decoded JSON body.
func (s *Supabase) do(ctx context.Context, method, path string, query url.Values, prefer string, body, out any) error {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(raw)
	}
	u := s.baseURL + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, u, reader)
	if err != nil {
		return err
	}
	// New-style keys (sb_secret_...) go in the apikey header only: they are
	// not JWTs, and Supabase documents that they must not be sent as a
	// bearer token. Legacy service_role keys are JWTs and are sent both
	// ways, which is also what a bare PostgREST expects.
	req.Header.Set("apikey", s.key)
	if strings.HasPrefix(s.key, "eyJ") {
		req.Header.Set("Authorization", "Bearer "+s.key)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if prefer != "" {
		req.Header.Set("Prefer", prefer)
	}
	resp, err := s.http.Do(req)
	if err != nil {
		return fmt.Errorf("supabase %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<10))
		return fmt.Errorf("supabase %s %s: %s: %s", method, path, resp.Status, strings.TrimSpace(string(msg)))
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

func (s *Supabase) insert(ctx context.Context, table string, row any) error {
	return s.do(ctx, http.MethodPost, "/"+table, nil, "return=minimal", row, nil)
}

func (s *Supabase) SaveSession(ctx context.Context, sess Session) error {
	if len(sess.State) == 0 {
		sess.State = json.RawMessage(`{}`)
	}
	q := url.Values{"on_conflict": {"id"}}
	return s.do(ctx, http.MethodPost, "/sessions", q, "resolution=merge-duplicates,return=minimal", sess, nil)
}

func (s *Supabase) LoadSession(ctx context.Context, id string) (*Session, error) {
	q := url.Values{"id": {"eq." + id}, "select": {"*"}, "limit": {"1"}}
	var rows []Session
	if err := s.do(ctx, http.MethodGet, "/sessions", q, "", nil, &rows); err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return &rows[0], nil
}

func (s *Supabase) SaveTurn(ctx context.Context, t Turn) error {
	// The columns are NOT NULL; a nil slice would be sent as JSON null.
	if t.Flags == nil {
		t.Flags = []string{}
	}
	if t.Trace == nil {
		t.Trace = []agent.Event{}
	}
	return s.insert(ctx, "turns", t)
}

func (s *Supabase) SaveResearch(ctx context.Context, r Research) error {
	if r.Sources == nil {
		r.Sources = []agent.Source{}
	}
	// Let the database assign id and created_at.
	row := map[string]any{
		"session_id": nullable(r.SessionID), "question": r.Question, "answer": r.Answer,
		"sources": r.Sources, "model": r.Model, "is_public": r.IsPublic,
	}
	return s.insert(ctx, "research_answers", row)
}

func (s *Supabase) ListResearch(ctx context.Context, limit int) ([]Research, error) {
	q := url.Values{
		"is_public": {"eq.true"},
		"select":    {"id,question,answer,sources,model,created_at"},
		"order":     {"created_at.desc"},
		"limit":     {strconv.Itoa(limit)},
	}
	var rows []Research
	err := s.do(ctx, http.MethodGet, "/research_answers", q, "", nil, &rows)
	return rows, err
}

func (s *Supabase) AddUsage(ctx context.Context, u Usage) error {
	args := map[string]any{
		"p_turns": u.Turns, "p_input_tokens": u.InputTokens, "p_output_tokens": u.OutputTokens,
		"p_web_searches": u.WebSearches, "p_cost_usd": u.CostUSD,
	}
	return s.do(ctx, http.MethodPost, "/rpc/add_usage", nil, "", args, nil)
}

func (s *Supabase) UsageToday(ctx context.Context) (Usage, error) {
	q := url.Values{"day": {"eq." + today()}, "select": {"*"}, "limit": {"1"}}
	var rows []Usage
	if err := s.do(ctx, http.MethodGet, "/usage_daily", q, "", nil, &rows); err != nil {
		return Usage{}, err
	}
	if len(rows) == 0 {
		return Usage{Day: today()}, nil
	}
	return rows[0], nil
}

func (s *Supabase) LogGuardrail(ctx context.Context, e GuardrailEvent) error {
	row := map[string]any{
		"session_id": nullable(e.SessionID), "ip_hash": e.IPHash, "kind": e.Kind, "detail": e.Detail,
	}
	return s.insert(ctx, "guardrail_events", row)
}

func (s *Supabase) SaveEvalRun(ctx context.Context, r EvalRun) error {
	return s.insert(ctx, "eval_runs", r)
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}
