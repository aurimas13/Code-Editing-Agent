package guardrails

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

var (
	ErrEmptyInput   = errors.New("message is empty")
	ErrInputTooLong = errors.New("message is too long")
)

// Phrases that commonly appear in attempts to override the system prompt.
// A match is recorded as telemetry and shown in the trace; it does not block
// the request. Blocking on phrases like these stops honest questions about
// prompt injection and does not stop an attacker, who can rephrase.
var injectionHints = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\b(ignore|disregard|forget)\b.{0,40}\b(previous|prior|above|earlier|all)\b.{0,40}\b(instructions?|rules?|prompts?)`),
	regexp.MustCompile(`(?i)\b(reveal|print|show|repeat|output)\b.{0,40}\b(system prompt|hidden instructions|initial instructions)`),
	regexp.MustCompile(`(?i)\byou are now\b.{0,60}\b(unrestricted|jailbroken|developer mode|dan)\b`),
	regexp.MustCompile(`(?i)</?(system|assistant)>`),
}

// CheckInput normalises a visitor's message and enforces the length limit.
// It returns the cleaned text and any heuristic flags.
func CheckInput(s string, maxChars int) (string, []string, error) {
	if !utf8.ValidString(s) {
		s = strings.ToValidUTF8(s, "")
	}
	// Drop control characters other than newline and tab. They have no use
	// in a chat message and are a common way to hide text from a reviewer.
	s = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return r
		}
		if unicode.IsControl(r) || r == 0x200B || r == 0x2060 || r == 0xFEFF { // zero-width characters
			return -1
		}
		return r
	}, s)
	s = strings.TrimSpace(s)
	if s == "" {
		return "", nil, ErrEmptyInput
	}
	if n := utf8.RuneCountInString(s); maxChars > 0 && n > maxChars {
		return "", nil, fmt.Errorf("%w: %d characters, limit is %d", ErrInputTooLong, n, maxChars)
	}
	var flags []string
	for _, re := range injectionHints {
		if re.MatchString(s) {
			flags = append(flags, "possible_prompt_injection")
			break
		}
	}
	return s, flags, nil
}

// Truncate shortens s to at most max bytes on a rune boundary and says so,
// so the model knows it is looking at part of a larger result.
func Truncate(s string, max int) (string, bool) {
	if max <= 0 || len(s) <= max {
		return s, false
	}
	cut := max
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + fmt.Sprintf("\n[truncated: showing %d of %d bytes]", cut, len(s)), true
}
