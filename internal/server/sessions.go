package server

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"sync"
	"sync/atomic"
	"time"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/aurimas13/Code-Editing-Agent/guide"
	"github.com/aurimas13/Code-Editing-Agent/internal/agent"
	"github.com/aurimas13/Code-Editing-Agent/internal/store"
	"github.com/aurimas13/Code-Editing-Agent/internal/workspace"
)

//go:embed seed/*
var seedFS embed.FS

// tutorialGoMod is the go.mod the tutorial produces. It is a constant rather
// than an embedded file because a file named go.mod would start a nested
// module inside this repository.
const tutorialGoMod = `module agent

go 1.24

require (
	github.com/anthropics/anthropic-sdk-go v1.78.0
	github.com/invopop/jsonschema v0.14.0
)
`

// SeedFiles is what every new workspace starts with.
func SeedFiles() map[string]string {
	files := map[string]string{
		"main.go": guide.Final(),
		"go.mod":  tutorialGoMod,
	}
	entries, err := fs.ReadDir(seedFS, "seed")
	if err != nil {
		panic(err) // embedded at build time
	}
	for _, e := range entries {
		data, err := seedFS.ReadFile("seed/" + e.Name())
		if err != nil {
			panic(err)
		}
		files[e.Name()] = string(data)
	}
	return files
}

func newSeededFS() (*workspace.MemFS, error) {
	return workspace.NewMemFS(workspace.DemoLimits, SeedFiles())
}

var (
	errNoSession = errors.New("session not found")
	errBadToken  = errors.New("invalid session token")
)

// turnView is one exchange as the browser shows it. It is kept so a page
// reload can redraw the conversation without replaying it.
type turnView struct {
	Mode    string         `json:"mode"`
	Input   string         `json:"input"`
	Reply   string         `json:"reply"`
	Error   string         `json:"error,omitempty"`
	Trace   []agent.Event  `json:"trace"`
	Usage   agent.Usage    `json:"usage"`
	Sources []agent.Source `json:"sources,omitempty"`
}

// sessionState is what is saved to the database so a session survives a
// server restart or deploy.
type sessionState struct {
	Conversation  []anthropic.MessageParam `json:"conversation"`
	Files         map[string]string        `json:"files"`
	Transcript    []turnView               `json:"transcript"`
	ContextTokens int64                    `json:"context_tokens"`
}

type session struct {
	// turn serialises turns: one visitor message is handled at a time.
	turn sync.Mutex
	// busy is set while a turn runs, so the session is not evicted from
	// memory in the middle of one.
	busy atomic.Bool

	id        string
	tokenHash string
	ipHash    string
	model     string
	created   time.Time

	mu            sync.Mutex // guards the fields below
	lastActive    time.Time
	conv          []anthropic.MessageParam
	fs            *workspace.MemFS
	transcript    []turnView
	turns         int
	contextTokens int64
}

func (s *session) record() (store.Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state, err := json.Marshal(sessionState{
		Conversation:  s.conv,
		Files:         s.fs.Snapshot(),
		Transcript:    s.transcript,
		ContextTokens: s.contextTokens,
	})
	if err != nil {
		return store.Session{}, err
	}
	return store.Session{
		ID: s.id, TokenHash: s.tokenHash, IPHash: s.ipHash, Model: s.model,
		TurnCount: s.turns, State: state, CreatedAt: s.created, LastActiveAt: s.lastActive,
	}, nil
}

// sessions holds live sessions in memory and falls back to the store.
type sessions struct {
	mu    sync.Mutex
	byID  map[string]*session
	ttl   time.Duration
	max   int
	store store.Store
}

func newSessions(st store.Store, ttl time.Duration, max int) *sessions {
	return &sessions{byID: map[string]*session{}, ttl: ttl, max: max, store: st}
}

// create starts a session and returns it with its bearer token. The token is
// shown to the browser once and only its hash is kept.
func (m *sessions) create(ipHash, model string) (*session, string, error) {
	id, err := newUUID()
	if err != nil {
		return nil, "", err
	}
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return nil, "", err
	}
	token := base64.RawURLEncoding.EncodeToString(tokenBytes)

	ws, err := newSeededFS()
	if err != nil {
		return nil, "", err
	}
	now := time.Now().UTC()
	s := &session{
		id: id, tokenHash: hashToken(token), ipHash: ipHash, model: model,
		created: now, lastActive: now, fs: ws,
	}
	m.mu.Lock()
	m.evictLocked(now)
	m.byID[id] = s
	m.mu.Unlock()
	return s, token, nil
}

var uuidRe = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// get returns the session for id if token is its bearer token.
func (m *sessions) get(ctx context.Context, id, token string) (*session, error) {
	// Anything that is not one of our IDs cannot exist; do not ask the
	// database about it.
	if !uuidRe.MatchString(id) {
		return nil, errNoSession
	}
	m.mu.Lock()
	s, ok := m.byID[id]
	m.mu.Unlock()
	if !ok {
		loaded, err := m.rehydrate(ctx, id)
		if err != nil {
			return nil, err
		}
		// Check the token before the session takes a place in memory, so
		// guessing IDs cannot fill the table or push live sessions out.
		if subtle.ConstantTimeCompare([]byte(hashToken(token)), []byte(loaded.tokenHash)) != 1 {
			return nil, errBadToken
		}
		m.mu.Lock()
		if existing, raced := m.byID[id]; raced {
			loaded = existing
		} else {
			m.evictLocked(time.Now())
			m.byID[id] = loaded
		}
		m.mu.Unlock()
		s = loaded
	}
	if subtle.ConstantTimeCompare([]byte(hashToken(token)), []byte(s.tokenHash)) != 1 {
		return nil, errBadToken
	}
	s.mu.Lock()
	s.lastActive = time.Now().UTC()
	s.mu.Unlock()
	return s, nil
}

func (m *sessions) rehydrate(ctx context.Context, id string) (*session, error) {
	rec, err := m.store.LoadSession(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("load session: %w", err)
	}
	if rec == nil || time.Since(rec.LastActiveAt) > m.ttl {
		return nil, errNoSession
	}
	var state sessionState
	if err := json.Unmarshal(rec.State, &state); err != nil {
		return nil, fmt.Errorf("decode session state: %w", err)
	}
	ws, err := workspace.NewMemFS(workspace.DemoLimits, state.Files)
	if err != nil {
		return nil, err
	}
	return &session{
		id: rec.ID, tokenHash: rec.TokenHash, ipHash: rec.IPHash, model: rec.Model,
		created: rec.CreatedAt, lastActive: rec.LastActiveAt,
		conv: state.Conversation, fs: ws, transcript: state.Transcript,
		turns: rec.TurnCount, contextTokens: state.ContextTokens,
	}, nil
}

// evictLocked drops expired sessions and, if still over capacity, the least
// recently used. A session with a turn in progress is never dropped: it
// would be reloaded from the store as a second copy with its own lock.
// Evicted sessions remain in the store until they expire.
func (m *sessions) evictLocked(now time.Time) {
	lastActive := func(s *session) time.Time {
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.lastActive
	}
	for id, s := range m.byID {
		if !s.busy.Load() && now.Sub(lastActive(s)) > m.ttl {
			delete(m.byID, id)
		}
	}
	for m.max > 0 && len(m.byID) >= m.max {
		var oldestID string
		var oldest time.Time
		for id, s := range m.byID {
			if s.busy.Load() {
				continue
			}
			if la := lastActive(s); oldestID == "" || la.Before(oldest) {
				oldestID, oldest = id, la
			}
		}
		if oldestID == "" {
			return // every session is mid-turn; allow going over rather than break one
		}
		delete(m.byID, oldestID)
	}
}

func (m *sessions) count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.byID)
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func newUUID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // RFC 4122 variant
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}
