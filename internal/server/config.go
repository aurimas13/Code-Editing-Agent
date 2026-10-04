// Package server exposes the agent over HTTP for the website.
//
// It is where a tutorial loop becomes something strangers can be allowed to
// use: every visitor gets an isolated in-memory workspace, every request is
// rate-limited and budgeted, and every step is streamed to the browser and
// written to the trace.
package server

import (
	"os"
	"strconv"
	"strings"
	"time"
)

// Config is the server's settings. The defaults are the public demo's limits,
// and every one can be changed from the environment without a rebuild.
type Config struct {
	Addr           string
	AllowedOrigins []string // exact origins, or patterns with one "*"

	Model           string
	ResearchEnabled bool

	// Per-request limits.
	MaxInputChars    int
	MaxOutputTokens  int64
	MaxRounds        int
	WebSearchMaxUses int64
	TurnTimeout      time.Duration

	// Per-session limits.
	MaxTurnsPerSession int
	MaxContextTokens   int64 // a session is "full" once a call's input exceeds this
	SessionTTL         time.Duration
	MaxSessions        int

	// Per-visitor limits (keyed by hashed address).
	TurnsPerMinute  int
	TurnsPerDay     int
	ResearchPerDay  int
	SessionsPerHour int

	// Global limits.
	DailyBudgetUSD     float64
	MaxConcurrentTurns int

	// How to find the caller's address. Both default to "trust nothing":
	// with neither set, the socket address is used, which is correct when
	// the server faces clients directly and is the proxy's address (so every
	// visitor shares one limit) when it does not. Set one of them to match
	// the platform:
	//
	// ClientIPHeader names a header the platform's proxy sets to the client
	// address and overwrites if the client sent its own, such as X-Real-IP
	// on Railway.
	//
	// TrustedProxyHops is how many reverse proxies sit in front of the
	// server. The address is taken that many entries from the right of
	// X-Forwarded-For; entries further left are client-controlled and
	// ignored.
	ClientIPHeader   string
	TrustedProxyHops int
	// RequestsPerMinute bounds all API calls from one address, including
	// ones that are rejected, so rejections cannot be used to generate load.
	RequestsPerMinute int
	// IPHashKey keys the hash applied to client addresses before they are
	// used or stored. If empty, a random key is generated at startup.
	IPHashKey string
}

// DefaultConfig returns the limits used by the public demo.
func DefaultConfig() Config {
	return Config{
		Addr:               ":8080",
		AllowedOrigins:     []string{"http://localhost:3000"},
		Model:              "claude-haiku-4-5",
		ResearchEnabled:    true,
		MaxInputChars:      2000,
		MaxOutputTokens:    1500,
		MaxRounds:          8,
		WebSearchMaxUses:   3,
		TurnTimeout:        120 * time.Second,
		MaxTurnsPerSession: 12,
		MaxContextTokens:   60_000,
		SessionTTL:         time.Hour,
		MaxSessions:        500,
		TurnsPerMinute:     6,
		TurnsPerDay:        40,
		ResearchPerDay:     8,
		SessionsPerHour:    10,
		DailyBudgetUSD:     3.00,
		MaxConcurrentTurns: 8,
		RequestsPerMinute:  90,
	}
}

// ConfigFromEnv overlays environment variables on the defaults.
func ConfigFromEnv() Config {
	c := DefaultConfig()
	if port := os.Getenv("PORT"); port != "" {
		c.Addr = ":" + port
	}
	if v := os.Getenv("ALLOWED_ORIGINS"); v != "" {
		c.AllowedOrigins = nil
		for _, o := range strings.Split(v, ",") {
			if o = strings.TrimSpace(o); o != "" {
				c.AllowedOrigins = append(c.AllowedOrigins, strings.TrimRight(o, "/"))
			}
		}
	}
	str(&c.Model, "AGENT_MODEL")
	str(&c.IPHashKey, "IP_HASH_KEY")
	str(&c.ClientIPHeader, "CLIENT_IP_HEADER")
	integer(&c.RequestsPerMinute, "REQUESTS_PER_MINUTE")
	boolean(&c.ResearchEnabled, "RESEARCH_ENABLED")
	integer(&c.MaxInputChars, "MAX_INPUT_CHARS")
	integer64(&c.MaxOutputTokens, "MAX_OUTPUT_TOKENS")
	integer(&c.MaxRounds, "MAX_ROUNDS")
	integer64(&c.WebSearchMaxUses, "WEB_SEARCH_MAX_USES")
	integer(&c.MaxTurnsPerSession, "MAX_TURNS_PER_SESSION")
	integer64(&c.MaxContextTokens, "MAX_CONTEXT_TOKENS")
	integer(&c.MaxSessions, "MAX_SESSIONS")
	integer(&c.TurnsPerMinute, "TURNS_PER_MINUTE")
	integer(&c.TurnsPerDay, "TURNS_PER_DAY")
	integer(&c.ResearchPerDay, "RESEARCH_PER_DAY")
	integer(&c.SessionsPerHour, "SESSIONS_PER_HOUR")
	integer(&c.MaxConcurrentTurns, "MAX_CONCURRENT_TURNS")
	integer(&c.TrustedProxyHops, "TRUSTED_PROXY_HOPS")
	if v, err := strconv.ParseFloat(os.Getenv("DAILY_BUDGET_USD"), 64); err == nil && v >= 0 {
		c.DailyBudgetUSD = v
	}
	if v, err := time.ParseDuration(os.Getenv("TURN_TIMEOUT")); err == nil && v > 0 {
		c.TurnTimeout = v
	}
	if v, err := time.ParseDuration(os.Getenv("SESSION_TTL")); err == nil && v > 0 {
		c.SessionTTL = v
	}
	return c
}

func str(dst *string, key string) {
	if v := os.Getenv(key); v != "" {
		*dst = v
	}
}

func boolean(dst *bool, key string) {
	if v, err := strconv.ParseBool(os.Getenv(key)); err == nil {
		*dst = v
	}
}

func integer(dst *int, key string) {
	if v, err := strconv.Atoi(os.Getenv(key)); err == nil && v >= 0 {
		*dst = v
	}
}

func integer64(dst *int64, key string) {
	if v, err := strconv.ParseInt(os.Getenv(key), 10, 64); err == nil && v >= 0 {
		*dst = v
	}
}
