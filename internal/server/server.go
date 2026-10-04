package server

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/aurimas13/Code-Editing-Agent/internal/agent"
	"github.com/aurimas13/Code-Editing-Agent/internal/guardrails"
	"github.com/aurimas13/Code-Editing-Agent/internal/llm"
	"github.com/aurimas13/Code-Editing-Agent/internal/store"
	"github.com/aurimas13/Code-Editing-Agent/internal/tools"
)

// Modes a visitor can choose.
const (
	ModeCode     = "code"
	ModeResearch = "research"
)

// Server is the HTTP API.
type Server struct {
	cfg      Config
	log      *slog.Logger
	store    store.Store
	sessions *sessions
	agents   map[string]*agent.Agent
	demo     bool

	budget          *guardrails.Budget
	perMinute       *guardrails.Limiter
	perDay          *guardrails.Limiter
	researchPerDay  *guardrails.Limiter
	sessionsPerHour *guardrails.Limiter
	requests        *guardrails.Limiter // every API call, per address
	guardrailLogs   *guardrails.Limiter // guardrail rows written, per address
	slots           chan struct{}
	ipKey           []byte

	// background tracks database writes started after a response, so tests
	// and graceful shutdown can wait for them. writeSlots bounds how many
	// can be in flight; beyond that, writes are dropped rather than queued.
	background sync.WaitGroup
	writeSlots chan struct{}

	libraryMu sync.Mutex
	library   []store.Research
	libraryAt time.Time
}

// New builds a server. demo marks that client is the scripted stand-in, not
// a real model; it is reported to the UI so visitors are told.
func New(cfg Config, client llm.Client, st store.Store, demo bool, log *slog.Logger) *Server {
	if log == nil {
		log = slog.Default()
	}
	registry := tools.Default()
	base := agent.Config{
		Model:            cfg.Model,
		MaxTokens:        cfg.MaxOutputTokens,
		MaxRounds:        cfg.MaxRounds,
		WebSearchMaxUses: cfg.WebSearchMaxUses,
		Now:              time.Now,
	}
	code := base
	code.System = agent.SystemPrompt(false, true)
	research := base
	research.System = agent.SystemPrompt(true, true)
	research.WebSearch = true

	key := []byte(cfg.IPHashKey)
	if len(key) == 0 {
		key = make([]byte, 32)
		_, _ = rand.Read(key)
	}
	if demo {
		cfg.ResearchEnabled = false // the scripted stand-in cannot search
	}

	s := &Server{
		cfg:      cfg,
		log:      log,
		store:    st,
		sessions: newSessions(st, cfg.SessionTTL, cfg.MaxSessions),
		agents: map[string]*agent.Agent{
			ModeCode:     agent.New(client, registry, code),
			ModeResearch: agent.New(client, registry, research),
		},
		demo:            demo,
		budget:          guardrails.NewBudget(cfg.DailyBudgetUSD),
		perMinute:       guardrails.NewLimiter(cfg.TurnsPerMinute, time.Minute),
		perDay:          guardrails.NewLimiter(cfg.TurnsPerDay, 24*time.Hour),
		researchPerDay:  guardrails.NewLimiter(cfg.ResearchPerDay, 24*time.Hour),
		sessionsPerHour: guardrails.NewLimiter(cfg.SessionsPerHour, time.Hour),
		requests:        guardrails.NewLimiter(cfg.RequestsPerMinute, time.Minute),
		guardrailLogs:   guardrails.NewLimiter(30, time.Hour),
		slots:           make(chan struct{}, max(cfg.MaxConcurrentTurns, 1)),
		writeSlots:      make(chan struct{}, 64),
		ipKey:           key,
	}

	// Resume today's spend so a restart does not reset the budget.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if usage, err := st.UsageToday(ctx); err != nil {
		log.Warn("could not load today's usage; budget starts at zero", "err", err)
	} else {
		s.budget.Seed(usage.CostUSD)
	}
	return s
}

// Wait blocks until background writes have finished.
func (s *Server) Wait() { s.background.Wait() }

// Handler returns the routed, middleware-wrapped handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	})
	mux.HandleFunc("GET /api/config", s.handleConfig)
	mux.HandleFunc("POST /api/sessions", s.handleCreateSession)
	mux.HandleFunc("GET /api/sessions/{id}", s.handleGetSession)
	mux.HandleFunc("POST /api/sessions/{id}/reset", s.handleResetSession)
	mux.HandleFunc("POST /api/sessions/{id}/messages", s.handleMessage)
	mux.HandleFunc("GET /api/research", s.handleResearch)
	return s.recoverer(s.logging(s.cors(securityHeaders(s.throttle(mux)))))
}

// throttle bounds how many API calls one address can make, whatever their
// outcome. The specific limits (turns, sessions, research) are stricter; this
// is the floor under them that stops rejected requests being used as load.
func (s *Server) throttle(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodOptions && strings.HasPrefix(r.URL.Path, "/api/") {
			if ok, retry := s.requests.Allow(s.clientHash(r)); !ok {
				writeLimited(w, "rate_limited", "Too many requests. Wait a moment and try again.", retry)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// ---- handlers -------------------------------------------------------------

func (s *Server) handleConfig(w http.ResponseWriter, _ *http.Request) {
	mode := "live"
	if s.demo {
		mode = "demo"
	}
	spent, limit := s.budget.Status()
	usedPct := 0
	if limit > 0 {
		usedPct = min(int(spent/limit*100), 100)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"mode":             mode,
		"model":            s.cfg.Model,
		"research_enabled": s.cfg.ResearchEnabled,
		"store":            s.store.Name(),
		"tools":            tools.Default().Names(),
		"limits": map[string]any{
			"max_input_chars":       s.cfg.MaxInputChars,
			"max_output_tokens":     s.cfg.MaxOutputTokens,
			"max_rounds":            s.cfg.MaxRounds,
			"max_turns_per_session": s.cfg.MaxTurnsPerSession,
			"turns_per_minute":      s.cfg.TurnsPerMinute,
			"turns_per_day":         s.cfg.TurnsPerDay,
			"research_per_day":      s.cfg.ResearchPerDay,
			"web_search_max_uses":   s.cfg.WebSearchMaxUses,
		},
		"budget": map[string]any{"exhausted": s.budget.Exhausted(), "used_pct": usedPct},
	})
}

func (s *Server) handleCreateSession(w http.ResponseWriter, r *http.Request) {
	ip := s.clientHash(r)
	if ok, retry := s.sessionsPerHour.Allow(ip); !ok {
		s.guardrail(r.Context(), "", ip, "rate_limit_sessions", "")
		writeLimited(w, "rate_limited", "You've started several sessions recently. Try again later.", retry)
		return
	}
	sess, token, err := s.sessions.create(ip, s.cfg.Model)
	if err != nil {
		s.log.Error("create session", "err", err)
		writeError(w, http.StatusInternalServerError, "internal", "Could not start a session.")
		return
	}
	s.persistSession(sess)
	writeJSON(w, http.StatusCreated, map[string]any{
		"id":         sess.id,
		"token":      token,
		"files":      fileViews(sess.fs.Snapshot()),
		"transcript": []turnView{},
		"turns_used": 0,
		"turns_max":  s.cfg.MaxTurnsPerSession,
	})
}

func (s *Server) handleGetSession(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.authed(w, r)
	if !ok {
		return
	}
	sess.mu.Lock()
	transcript := append([]turnView{}, sess.transcript...)
	turns, ws := sess.turns, sess.fs
	sess.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{
		"id":         sess.id,
		"files":      fileViews(ws.Snapshot()),
		"transcript": transcript,
		"turns_used": turns,
		"turns_max":  s.cfg.MaxTurnsPerSession,
	})
}

// handleResetSession clears the conversation and restores the starting
// files. The turn count is kept: a reset is a clean slate, not a way around
// the session limit.
func (s *Server) handleResetSession(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.authed(w, r)
	if !ok {
		return
	}
	if !sess.turn.TryLock() {
		writeError(w, http.StatusConflict, "turn_in_progress", "Wait for the current reply to finish.")
		return
	}
	defer sess.turn.Unlock()

	fresh, err := newSeededFS()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Could not reset the workspace.")
		return
	}
	sess.mu.Lock()
	sess.conv, sess.transcript, sess.contextTokens, sess.fs = nil, nil, 0, fresh
	turns := sess.turns
	sess.mu.Unlock()
	s.persistSession(sess)
	writeJSON(w, http.StatusOK, map[string]any{
		"id":         sess.id,
		"files":      fileViews(fresh.Snapshot()),
		"transcript": []turnView{},
		"turns_used": turns,
		"turns_max":  s.cfg.MaxTurnsPerSession,
	})
}

// handleResearch serves the published answers. The list changes only when a
// person publishes one, so it is cached briefly rather than queried per view.
func (s *Server) handleResearch(w http.ResponseWriter, r *http.Request) {
	s.libraryMu.Lock()
	defer s.libraryMu.Unlock()
	if s.library == nil || time.Since(s.libraryAt) > 30*time.Second {
		answers, err := s.store.ListResearch(r.Context(), 30)
		if err != nil {
			s.log.Error("list research", "err", err)
			writeError(w, http.StatusBadGateway, "store_unavailable", "The research library is unavailable right now.")
			return
		}
		if answers == nil {
			answers = []store.Research{}
		}
		s.library, s.libraryAt = answers, time.Now()
	}
	writeJSON(w, http.StatusOK, map[string]any{"answers": s.library})
}

// authed resolves the session named in the path using the bearer token.
func (s *Server) authed(w http.ResponseWriter, r *http.Request) (*session, bool) {
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if token == "" || token == r.Header.Get("Authorization") {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Missing session token.")
		return nil, false
	}
	sess, err := s.sessions.get(r.Context(), r.PathValue("id"), token)
	switch {
	case errors.Is(err, errNoSession):
		writeError(w, http.StatusNotFound, "session_not_found", "This session has expired. Start a new one.")
	case errors.Is(err, errBadToken):
		writeError(w, http.StatusUnauthorized, "unauthorized", "Invalid session token.")
	case err != nil:
		s.log.Error("load session", "err", err)
		writeError(w, http.StatusBadGateway, "store_unavailable", "Could not load the session.")
	default:
		return sess, true
	}
	return nil, false
}

// ---- persistence ----------------------------------------------------------

// async runs a store write after the response, bounded by a timeout. A
// failed write is logged and dropped: losing a trace row must never fail a
// visitor's request.
func (s *Server) async(name string, fn func(ctx context.Context) error) {
	select {
	case s.writeSlots <- struct{}{}:
	default:
		s.log.Warn("store write dropped: too many in flight", "op", name)
		return
	}
	s.background.Add(1)
	go func() {
		defer s.background.Done()
		defer func() { <-s.writeSlots }()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := fn(ctx); err != nil {
			s.log.Error("store write failed", "op", name, "err", err)
		}
	}()
}

func (s *Server) persistSession(sess *session) {
	rec, err := sess.record()
	if err != nil {
		s.log.Error("encode session", "err", err)
		return
	}
	s.async("save_session", func(ctx context.Context) error { return s.store.SaveSession(ctx, rec) })
}

// guardrail records that a check intervened. Rows are capped per address:
// the first few rejections from a visitor are worth keeping, the thousandth
// identical one is not.
func (s *Server) guardrail(_ context.Context, sessionID, ipHash, kind, detail string) {
	s.log.Info("guardrail", "kind", kind, "detail", detail, "session", sessionID)
	if ok, _ := s.guardrailLogs.Allow(ipHash); !ok {
		return
	}
	ev := store.GuardrailEvent{SessionID: sessionID, IPHash: ipHash, Kind: kind, Detail: detail}
	s.async("log_guardrail", func(ctx context.Context) error { return s.store.LogGuardrail(ctx, ev) })
}

// ---- client identity ------------------------------------------------------

// clientHash returns a keyed hash of the caller's address. Limits are
// applied to the hash and only the hash is stored.
func (s *Server) clientHash(r *http.Request) string {
	mac := hmac.New(sha256.New, s.ipKey)
	mac.Write([]byte(clientIP(r, s.cfg.ClientIPHeader, s.cfg.TrustedProxyHops)))
	return hex.EncodeToString(mac.Sum(nil))[:32]
}

// clientIP finds the caller's address using only sources the operator has
// said to trust (see Config). Anything a client could have typed itself,
// such as the left-hand entries of X-Forwarded-For, is ignored, or rate
// limits could be dodged by changing a header.
func clientIP(r *http.Request, header string, trustedHops int) string {
	if header != "" {
		if v := strings.TrimSpace(r.Header.Get(header)); v != "" {
			return limiterKey(v)
		}
	}
	if trustedHops > 0 {
		var parts []string
		for _, h := range r.Header.Values("X-Forwarded-For") {
			for _, p := range strings.Split(h, ",") {
				if p = strings.TrimSpace(p); p != "" {
					parts = append(parts, p)
				}
			}
		}
		if len(parts) >= trustedHops {
			return limiterKey(parts[len(parts)-trustedHops])
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return limiterKey(r.RemoteAddr)
	}
	return limiterKey(host)
}

// limiterKey is the unit limits apply to. For IPv6 that is the /64 network:
// one subscriber normally holds a whole /64, so limiting single addresses
// would give them 2^64 identities.
func limiterKey(addr string) string {
	ip := net.ParseIP(addr)
	if ip == nil || ip.To4() != nil {
		return addr
	}
	return ip.Mask(net.CIDRMask(64, 128)).String() + "/64"
}

// ---- middleware -----------------------------------------------------------

func (s *Server) cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" && s.originAllowed(origin) {
			h := w.Header()
			h.Set("Access-Control-Allow-Origin", origin)
			h.Add("Vary", "Origin")
			h.Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			h.Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
			h.Set("Access-Control-Max-Age", "600")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) originAllowed(origin string) bool {
	for _, allowed := range s.cfg.AllowedOrigins {
		if matchOrigin(allowed, origin) {
			return true
		}
	}
	return false
}

// matchOrigin compares an origin with an allowed entry. An entry may contain
// one "*", which stands for part of a single host label, so preview deploys
// can be allowed as https://code-editing-agent-*.vercel.app without allowing
// every site on that domain.
func matchOrigin(pattern, origin string) bool {
	if pattern == origin {
		return true
	}
	prefix, suffix, ok := strings.Cut(pattern, "*")
	if !ok || len(origin) <= len(prefix)+len(suffix) ||
		!strings.HasPrefix(origin, prefix) || !strings.HasSuffix(origin, suffix) {
		return false
	}
	label := origin[len(prefix) : len(origin)-len(suffix)]
	return !strings.ContainsAny(label, "./:@")
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
		h.Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

// Unwrap lets http.ResponseController reach the real writer to flush.
func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (s *Server) logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(sw, r)
		if r.URL.Path == "/healthz" {
			return
		}
		// Paths contain session ids but never tokens; tokens travel in the
		// Authorization header, which is not logged.
		s.log.Info("request", "method", r.Method, "path", r.URL.Path, "status", sw.status,
			"ms", time.Since(start).Milliseconds())
	})
}

func (s *Server) recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				s.log.Error("panic", "value", v, "path", r.URL.Path)
				writeError(w, http.StatusInternalServerError, "internal", "Something went wrong.")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// ---- responses ------------------------------------------------------------

type fileView struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

func fileViews(files map[string]string) []fileView {
	out := make([]fileView, 0, len(files))
	for p, c := range files {
		out = append(out, fileView{Path: p, Content: c})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{"error": map[string]any{"code": code, "message": message}})
}

func writeLimited(w http.ResponseWriter, code, message string, retry time.Duration) {
	secs := int(retry.Seconds()) + 1
	w.Header().Set("Retry-After", strconv.Itoa(secs))
	writeJSON(w, http.StatusTooManyRequests, map[string]any{
		"error": map[string]any{"code": code, "message": message, "retry_after_seconds": secs},
	})
}
