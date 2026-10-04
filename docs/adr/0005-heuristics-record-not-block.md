# ADR 0005: Heuristic input filters record; they do not block

Status: accepted

## Context

It is common to screen user input for prompt-injection phrases ("ignore all
previous instructions") and reject matches.

## Decision

Run a small set of patterns over each message. On a match, add a flag to the
turn, show it in the visitor's trace, and store it for review. Let the
request proceed.

Blocking is reserved for hard, countable limits: message size, rate, turns,
budget, workspace quotas.

## Why

- A phrase filter stops people asking honest questions about prompt
  injection, on a site whose purpose is teaching how agents work.
- It does not stop an attacker, who rephrases, translates or encodes.
- It creates a false sense of safety that discourages the real control,
  which is limiting what the agent can reach (ADR 0001).

## Consequences

- The flags are useful as data: they show what people try.
- Nothing in the safety argument depends on the filter. If the patterns were
  deleted, no eval would fail.
- Credential redaction is a different case and does act: it rewrites
  matching text, because the cost of a false positive is a visible
  `[REDACTED]` and the cost of a false negative is a leaked key.
