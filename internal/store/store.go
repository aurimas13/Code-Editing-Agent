// Package store persists sessions, traces, research answers, and usage.
//
// The server works against the Store interface. Supabase is the production
// implementation; Memory is used for local development and tests, so the
// agent runs with no database at all.
package store

import (
	"context"
	"encoding/json"
	"sort"
	"sync"
	"time"

	"github.com/aurimas13/Code-Editing-Agent/internal/agent"
)

// Session is a visitor's conversation and workspace.
type Session struct {
	ID string `json:"id"`
	// TokenHash is the SHA-256 of the bearer token the browser holds. The
	// token itself is never stored, so a database leak does not let anyone
	// resume someone else's session.
	TokenHash string `json:"token_hash"`
	// IPHash is a keyed hash of the client address, for rate limiting and
	// abuse review. Raw addresses are not stored.
	IPHash       string          `json:"ip_hash"`
	Model        string          `json:"model"`
	TurnCount    int             `json:"turn_count"`
	State        json.RawMessage `json:"state"`
	CreatedAt    time.Time       `json:"created_at"`
	LastActiveAt time.Time       `json:"last_active_at"`
}

// Turn is one user message and everything the agent did in response.
type Turn struct {
	SessionID    string        `json:"session_id"`
	Index        int           `json:"idx"`
	Mode         string        `json:"mode"`
	Model        string        `json:"model"`
	Input        string        `json:"input"`
	Reply        string        `json:"reply"`
	StopReason   string        `json:"stop_reason"`
	Rounds       int           `json:"rounds"`
	InputTokens  int64         `json:"input_tokens"`
	OutputTokens int64         `json:"output_tokens"`
	WebSearches  int64         `json:"web_searches"`
	CostUSD      float64       `json:"cost_usd"`
	LatencyMS    int64         `json:"latency_ms"`
	Flags        []string      `json:"flags"`
	Error        string        `json:"error,omitempty"`
	Trace        []agent.Event `json:"trace"`
}

// Research is a question answered with web search.
type Research struct {
	ID        string         `json:"id,omitempty"`
	SessionID string         `json:"session_id,omitempty"`
	Question  string         `json:"question"`
	Answer    string         `json:"answer"`
	Sources   []agent.Source `json:"sources"`
	Model     string         `json:"model"`
	IsPublic  bool           `json:"is_public"`
	CreatedAt time.Time      `json:"created_at,omitzero"`
}

// Usage is a day's consumption across all visitors.
type Usage struct {
	Day          string  `json:"day,omitempty"`
	Turns        int64   `json:"turns"`
	InputTokens  int64   `json:"input_tokens"`
	OutputTokens int64   `json:"output_tokens"`
	WebSearches  int64   `json:"web_searches"`
	CostUSD      float64 `json:"cost_usd"`
}

// GuardrailEvent records a check that fired outside a turn (rate limits,
// rejected input) or one worth keeping for review.
type GuardrailEvent struct {
	SessionID string `json:"session_id,omitempty"`
	IPHash    string `json:"ip_hash"`
	Kind      string `json:"kind"`
	Detail    string `json:"detail,omitempty"`
}

// EvalRun is the outcome of one run of an eval suite.
type EvalRun struct {
	Suite  string          `json:"suite"`
	Model  string          `json:"model"`
	GitSHA string          `json:"git_sha,omitempty"`
	Passed int             `json:"passed"`
	Failed int             `json:"failed"`
	Total  int             `json:"total"`
	Report json.RawMessage `json:"report"`
}

// Store is the persistence the server needs. Implementations must be safe
// for concurrent use.
type Store interface {
	SaveSession(ctx context.Context, s Session) error
	// LoadSession returns nil, nil when the session does not exist.
	LoadSession(ctx context.Context, id string) (*Session, error)
	SaveTurn(ctx context.Context, t Turn) error
	SaveResearch(ctx context.Context, r Research) error
	// ListResearch returns published answers, newest first.
	ListResearch(ctx context.Context, limit int) ([]Research, error)
	AddUsage(ctx context.Context, u Usage) error
	UsageToday(ctx context.Context) (Usage, error)
	LogGuardrail(ctx context.Context, e GuardrailEvent) error
	SaveEvalRun(ctx context.Context, r EvalRun) error
	// Name identifies the backend in /api/config and logs.
	Name() string
}

// Memory is an in-process Store.
type Memory struct {
	mu         sync.Mutex
	sessions   map[string]Session
	Turns      []Turn
	Researches []Research
	Guardrails []GuardrailEvent
	EvalRuns   []EvalRun
	usage      map[string]Usage
}

// NewMemory returns an empty in-memory store.
func NewMemory() *Memory {
	return &Memory{sessions: map[string]Session{}, usage: map[string]Usage{}}
}

func today() string { return time.Now().UTC().Format("2006-01-02") }

func (m *Memory) Name() string { return "memory" }

func (m *Memory) SaveSession(_ context.Context, s Session) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessions[s.ID] = s
	return nil
}

func (m *Memory) LoadSession(_ context.Context, id string) (*Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[id]
	if !ok {
		return nil, nil
	}
	return &s, nil
}

func (m *Memory) SaveTurn(_ context.Context, t Turn) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Turns = capped(append(m.Turns, t))
	return nil
}

func (m *Memory) SaveResearch(_ context.Context, r Research) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if r.CreatedAt.IsZero() {
		r.CreatedAt = time.Now().UTC()
	}
	m.Researches = append(m.Researches, r)
	return nil
}

func (m *Memory) ListResearch(_ context.Context, limit int) ([]Research, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Research
	for _, r := range m.Researches {
		if r.IsPublic {
			r.SessionID = ""
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (m *Memory) AddUsage(_ context.Context, u Usage) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	day := today()
	cur := m.usage[day]
	cur.Day = day
	cur.Turns += u.Turns
	cur.InputTokens += u.InputTokens
	cur.OutputTokens += u.OutputTokens
	cur.WebSearches += u.WebSearches
	cur.CostUSD += u.CostUSD
	m.usage[day] = cur
	return nil
}

func (m *Memory) UsageToday(_ context.Context) (Usage, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.usage[today()], nil
}

// memoryCap bounds each in-memory list. Memory is for development and tests;
// a long-running process without a database keeps the most recent entries.
const memoryCap = 5000

func capped[T any](list []T) []T {
	if len(list) > memoryCap {
		return append(list[:0:0], list[len(list)-memoryCap:]...)
	}
	return list
}

func (m *Memory) LogGuardrail(_ context.Context, e GuardrailEvent) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Guardrails = capped(append(m.Guardrails, e))
	return nil
}

func (m *Memory) SaveEvalRun(_ context.Context, r EvalRun) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.EvalRuns = append(m.EvalRuns, r)
	return nil
}
