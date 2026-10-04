# ADR 0004: Talk to Supabase over REST, with no database driver

Status: accepted

## Context

The server writes a session snapshot, a turn with its trace, a usage
increment and occasional guardrail events. It reads one session by ID,
today's usage, and a short list of published answers. There are no joins and
no transactions spanning statements.

## Decision

Use Supabase's REST interface (PostgREST) through `net/http`. Authenticate
with the project's secret key, held only in the server's environment. Keep
the browser away from the database entirely.

Enable row level security on every table regardless. Revoke the default
grants from `anon` and `authenticated`, then grant back only: reading
published research answers (specific columns) and reading eval runs.

The one operation that needs atomicity, adding to today's usage, is a SQL
function (`add_usage`) doing `insert ... on conflict do update`.

## Alternatives considered

- **`pgx` and a connection pool.** The right tool for transactional
  workloads. Here it would add a dependency, pool sizing, and a database
  password to manage, for five simple statements.
- **`supabase-js` in the browser with RLS as the only guard.** Common, and
  it makes the publishable key and RLS policies the entire security model
  for visitor data. Keeping one server-side writer is simpler to reason
  about and to audit.

## Consequences

- The Go module has two direct dependencies: the Anthropic SDK and the JSON
  schema generator.
- Every write is one HTTPS round trip. Writes happen after the response is
  sent, so visitors do not wait for them, and a failed write is logged, not
  surfaced.
- The access rules are tested against a real Postgres and PostgREST
  (`TestIntegrationPublicKeyAccess`): twelve things the public key must not
  be able to do, two it must.
- If the workload grows joins or multi-statement transactions, the `Store`
  interface is where a `pgx` implementation would go.
