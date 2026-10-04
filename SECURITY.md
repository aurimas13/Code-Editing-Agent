# Security

This project lets anonymous visitors direct a language model that has tools.
This file says what it defends against, how, and what is out of scope.

## Reporting a problem

Open a private security advisory on the GitHub repository (Security tab →
Report a vulnerability). Please do not open a public issue for something
exploitable.

## The model in one paragraph

The agent's reach is the security boundary. It has three tools that read,
list and edit text files in a workspace it cannot leave, and on the public
site that workspace is an in-memory map belonging to one session. It has no
tool that executes code, opens a network connection, or reads the server's
environment. Prompts and input filters are used to make the agent behave
well; they are not relied on to keep it safe. See
[ADR 0001](docs/adr/0001-capability-over-prompt.md).

## Controls and where they live

| Risk | Control | Code | Checked by |
| --- | --- | --- | --- |
| Path traversal, absolute paths, `~`, NUL bytes | One validator that rejects | `internal/workspace/path.go` | `evals/01` sandbox cases |
| Symlink escape on a real disk | `os.Root` | `internal/workspace/diskfs.go` | `TestDiskFSConfinement` |
| Reading secrets inside the project (terminal) | Deny list: `.env`, key files, `.git`, `.ssh` | `internal/workspace/path.go` | `TestSensitive` |
| Workspace exhaustion | File count, file size, total size quotas | `internal/workspace/memfs.go` | `quota-*` evals |
| Destructive edits | Unique-match rule; empty search text refused on existing files | `internal/tools/files.go` | `evals/01` edit-rules cases |
| Runaway loop | Round limit, forced text answer on the last round, turn timeout | `internal/agent/agent.go` | `round-limit-stops-a-runaway` |
| Cost | Output cap; per-session, per-visitor and global daily limits | `internal/server/chat.go`, `internal/guardrails/limits.go` | `TestDailyBudgetStopsModelCalls`, `TestPerVisitorRateLimit` |
| Rate-limit evasion via a forged address header | No header is trusted by default; the operator names the one the platform sets. IPv6 is limited per /64 | `internal/server/server.go` | `TestClientIP`, `TestForgedForwardedHeaderDoesNotDodgeLimits` |
| Rejected requests used as load | A per-address cap on all API calls; guardrail rows capped per address; background writes bounded | `internal/server/server.go` | `TestRequestThrottleCoversRejectedRequests` |
| A stalled browser holding a turn slot | Write deadline on the stream; a failed write cancels the turn | `internal/server/chat.go` | `TestStreamStopsWhenTheClientStopsReading` |
| Session ID guessing | Malformed IDs never reach the database; the token is checked before a session is loaded into memory | `internal/server/sessions.go` | `TestBogusSessionIDsNeverReachTheStore` |
| Cross-session access | Random session ID plus a 256-bit bearer token, stored hashed | `internal/server/sessions.go` | `TestSessionsAreIsolated` |
| Credentials reaching the model, browser or database | Redaction of visitor messages and file-tool results before the model sees them, and of replies while streaming and before storage (limits below) | `internal/guardrails/redact.go` | `credential-in-file-never-reaches-model`, `TestCredentialInMessageNeverReachesModelOrStore`, `TestCredentialInReplyIsRedactedWhileStreamingAndInHistory`, `TestStreamRedactorNeverReleasesACredential` |
| Public database key misuse | RLS on every table; default grants revoked | `supabase/migrations` | `TestIntegrationPublicKeyAccess` |
| Model output rendered as markup | Replies are rendered to React elements, never HTML strings; links only from http(s) URLs | `web/src/components/Markdown.tsx` | by construction |
| Running model-written code | Not done on the server. The Run button uses a browser Worker with a timeout; the page's CSP, which the worker inherits, allows connections only to the site and its API | `web/src/lib/runner.ts`, `web/next.config.ts` | by construction |
| Unreviewed answers published | Research answers are stored private; only a person can publish | `internal/server/chat.go` | `TestSupabaseRequests` |

## Data

- Conversations, traces and workspace files are stored to review how the
  agent behaves, and deleted after 30 days by a scheduled job.
- Client addresses are stored only as a keyed hash. Session tokens are stored
  only as a SHA-256 hash.
- The site sets no cookies and loads no third-party scripts or fonts.

## Out of scope and known gaps

- **Denial of service at the network level.** That is the hosting platform's
  job; the application only bounds its own work.
- **A determined attacker changing addresses** can exceed per-visitor limits.
  The global daily budget still applies.
- **Redaction is pattern-based and partial.** Secrets with no recognisable
  shape pass through. Text the model writes into a file with `edit_file` is
  stored as written, and web search results are not inspected. Because
  visitor messages and file contents are redacted before the model sees
  them, a credential in either place should not reach the model to be
  repeated; the reply-side redaction is a second layer, not a guarantee.
- **The site's CSP allows inline scripts** (`script-src 'self'
  'unsafe-inline'`), which Next.js needs without a nonce setup. The
  protection against injected markup is that model output is never rendered
  as HTML.
- **Multiple server instances** would each keep their own counters. The
  deployment is pinned to one replica.
- **The quality or truth of model output.** Research answers carry sources
  and are reviewed before publishing; chat replies are not reviewed.
