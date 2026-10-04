# ADR 0003: Test against a fake API over the wire, not a mocked interface

Status: accepted

## Context

The loop's correctness depends on details of the Messages API: content
blocks, `tool_use` and `tool_result` pairing, streaming deltas, stop reasons.
Tests need a model that behaves predictably.

The loop already depends on a small interface (`llm.Client`), so the easy
path is a mock that returns hand-built `anthropic.Message` values.

## Decision

Implement the fake one layer lower. `internal/llm/fakellm` is an HTTP server
that speaks the Messages API, including server-sent events with partial JSON
for tool input. Tests point the real SDK client at it with
`option.WithBaseURL`.

Use the same fake, with a small rule-based responder, as "demo mode": the
server and CLI run against it when no API key is set.

## Alternatives considered

- **Mock `llm.Client`.** Fewer lines. But SDK response types are populated by
  the SDK's own JSON decoding; hand-built ones do not round-trip through
  `ToParam()`, so the mock would exercise a path production never takes.
  Streaming accumulation would not be exercised at all.
- **Record and replay real responses.** Faithful, but recordings go stale,
  contain account metadata, and cannot express "the model misbehaves in this
  specific way", which is what most loop tests need.

## Consequences

- Tests cover the SDK client, the stream accumulator, and conversation
  serialisation exactly as shipped. `TestConversationSurvivesJSONRoundTrip`
  exists because of this.
- Hostile model behaviour is easy to script: unknown tools, endless tool
  calls, truncated calls, API errors (`evals/02-agent-loop.json`).
- Anyone can clone the repository and see the full stack work with no
  account. The UI says clearly when the stand-in is answering.
- The fake implements only what the agent uses. It can imitate the blocks
  the server-side web search produces, so the loop's handling of them
  (counting searches, collecting and citing sources, resending the blocks) is
  tested deterministically. It does not search. Demo mode therefore has
  research switched off, and whether the real model searches well is covered
  only by the live suite.
