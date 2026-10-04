// Package guardrails holds the checks that sit around the model: what goes
// in, what comes out, how often, and how much it may cost.
//
// None of these are the security boundary. The boundary is capability: the
// agent has three file tools scoped to a sandbox and nothing else (see
// package workspace). Guardrails limit cost and abuse and keep accidents out
// of logs; they are written on the assumption that a determined prompt will
// get past any text filter.
package guardrails

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

type pattern struct {
	kind string
	re   *regexp.Regexp
}

// Credential shapes that should never reach the model, the browser, or the
// database. The list favours precision: every pattern has a distinctive
// prefix, so ordinary code and prose are left alone.
var secretPatterns = []pattern{
	// A key block with no END marker (a truncated file, or one still
	// streaming) is redacted to the end of the text.
	{"private-key", regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----[\s\S]*?(?:-----END [A-Z ]*PRIVATE KEY-----|\z)`)},
	{"anthropic-key", regexp.MustCompile(`sk-ant-[A-Za-z0-9_\-]{20,}`)},
	{"openai-key", regexp.MustCompile(`sk-(?:proj-)?[A-Za-z0-9_\-]{32,}`)},
	{"github-token", regexp.MustCompile(`(?:ghp|gho|ghu|ghs|ghr)_[A-Za-z0-9]{36,}|github_pat_[A-Za-z0-9_]{40,}`)},
	{"aws-access-key", regexp.MustCompile(`\b(?:AKIA|ASIA)[0-9A-Z]{16}\b`)},
	{"supabase-secret", regexp.MustCompile(`sb_secret_[A-Za-z0-9_\-]{20,}`)},
	{"slack-token", regexp.MustCompile(`xox[abprs]-[A-Za-z0-9\-]{10,}`)},
	{"jwt", regexp.MustCompile(`\beyJ[A-Za-z0-9_\-]{10,}\.eyJ[A-Za-z0-9_\-]{10,}\.[A-Za-z0-9_\-]{10,}`)},
}

// Redact replaces anything that looks like a credential with a placeholder
// and reports which kinds it found.
func Redact(s string) (string, []string) {
	var kinds []string
	for _, p := range secretPatterns {
		if p.re.MatchString(s) {
			kinds = append(kinds, p.kind)
			s = p.re.ReplaceAllString(s, "[REDACTED:"+p.kind+"]")
		}
	}
	return s, kinds
}

// streamHoldback is how many bytes of a streaming reply are held back until
// more text arrives. It is longer than the longest prefix any pattern above
// needs before it starts matching, so a credential is recognised before its
// first character would be released. A JWT is the exception: it is only
// recognisable once its third segment begins, however long the first two
// are, so a trailing word that starts like one is held back whole.
const streamHoldback = 96

// StreamRedactor redacts text that arrives in pieces, for replies that are
// shown as they are generated. Write returns the text that is safe to show
// so far; Flush returns the rest once the reply is complete.
type StreamRedactor struct {
	raw     strings.Builder
	emitted int // bytes of the redacted text already returned
}

func (r *StreamRedactor) Write(delta string) string {
	r.raw.WriteString(delta)
	red, _ := Redact(r.raw.String())
	safe := len(red) - streamHoldback
	if word := strings.LastIndexAny(red, " \t\n") + 1; word < safe && strings.HasPrefix(red[word:], "eyJ") {
		safe = word
	}
	for safe > r.emitted && !utf8.RuneStart(red[safe]) {
		safe--
	}
	if safe <= r.emitted {
		return ""
	}
	out := red[r.emitted:safe]
	r.emitted = safe
	return out
}

func (r *StreamRedactor) Flush() string {
	red, _ := Redact(r.raw.String())
	if len(red) <= r.emitted {
		return ""
	}
	out := red[r.emitted:]
	r.emitted = len(red)
	return out
}
