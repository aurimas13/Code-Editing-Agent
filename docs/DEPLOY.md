# Deploying

Three hosted pieces: the database on Supabase, the Go API on Railway, the
website on Vercel. Allow about thirty minutes the first time.

```
browser ──► Vercel (web/)             static Next.js site
   │
   └──────► Railway (Dockerfile)      Go API ──► Anthropic API
                                         └────► Supabase (Postgres over REST)
```

Nothing below needs a secret to be pasted anywhere except the dashboard of
the service that uses it.

## 1. Supabase

1. Create a project. Any region close to the API works; the project was
   designed around `eu-central-1`.
2. Apply the migrations in order, in the SQL editor or with the CLI:

   ```bash
   supabase link --project-ref <ref>
   supabase db push
   ```

   - `20261004000001_init.sql` creates the tables, row level security,
     grants and functions.
   - `20261004000002_retention_schedule.sql` schedules the nightly purge
     with pg_cron. If you would rather not enable pg_cron, skip it and call
     `select public.purge_expired();` from a scheduler you already have.
3. From **Settings → API keys**, copy the project URL and a **secret** key
   (`sb_secret_…`, or the legacy `service_role` key). The publishable key is
   not used by this project.
4. Run the security advisor (**Advisors → Security**). The expected result is
   no errors. It may note that four tables have RLS enabled and no policies;
   that is the design: no policy means no access for the public roles.

## 2. Railway (the API)

1. **New project → Deploy from GitHub repo** and pick this repository.
   Railway reads `railway.json`, builds the `Dockerfile`, and health-checks
   `/healthz`.
2. Set variables on the service:

   | Variable | Value |
   | --- | --- |
   | `ANTHROPIC_API_KEY` | your key |
   | `SUPABASE_URL` | `https://<ref>.supabase.co` |
   | `SUPABASE_SECRET_KEY` | the secret key from step 1 |
   | `ALLOWED_ORIGINS` | `https://code.aurimas.io,https://code-editing-agent-*.vercel.app` |
   | `IP_HASH_KEY` | output of `openssl rand -hex 32` |
   | `AGENT_MODEL` | `claude-haiku-4-5` (default) |
   | `DAILY_BUDGET_USD` | `3` (default) |

   | `CLIENT_IP_HEADER` | `X-Real-IP` |

   `PORT` is set by Railway. `CLIENT_IP_HEADER` matters: by default the
   server trusts no address header, and behind a proxy that means every
   visitor appears to come from the proxy and shares one rate limit.
   Railway's proxy sets `X-Real-IP` to the client address
   ([Railway docs](https://docs.railway.com/networking/public-networking/specs-and-limits)).
   The smoke test below checks that a client cannot forge it.
3. **Settings → Networking → Generate domain.** Check it:

   ```bash
   curl https://<api-domain>/healthz        # {"ok":true}
   curl https://<api-domain>/api/config     # "mode":"live","store":"supabase"
   ```

   If `mode` is `demo`, the API key is missing. If `store` is `memory`, the
   Supabase variables are missing.
4. Keep **replicas at 1**. Rate limits and live sessions are held in the
   memory of one process (see the README, "What this does not do").

## 3. Vercel (the website)

1. **Add new → Project**, import the repository, and set **Root Directory**
   to `web`. The framework is detected as Next.js.
2. Add one environment variable: `NEXT_PUBLIC_API_URL` = the Railway URL from
   step 2 (no trailing slash). It is compiled into the site and into its
   Content-Security-Policy, so redeploy after changing it.
3. Deploy, then **Settings → Domains → Add** `code.aurimas.io`.

## 4. DNS (Namecheap)

In **Advanced DNS** for `aurimas.io`, add the record Vercel shows for the
domain. It is normally:

| Type | Host | Value |
| --- | --- | --- |
| CNAME | `code` | `cname.vercel-dns.com.` |

Vercel issues the certificate once the record resolves.

## 5. Anthropic Console

- **Web search** must be enabled for the organisation, or research mode
  returns an error. To switch research off instead, set
  `RESEARCH_ENABLED=false` on the API.
- Put the key in its own workspace and give that workspace a **monthly spend
  limit**. The server's daily budget is the first line of defence; this is
  the one that still holds if the server is misconfigured.

## 6. Smoke test

- [ ] `https://code.aurimas.io/playground` shows "Live model".
- [ ] "Solve the riddle" answers "a horse" and the X-ray shows `read_file`.
- [ ] "Fix a bug" edits `greet.js`; Run prints `Hello, world!`.
- [ ] "Try to break out" shows a `workspace boundary` guardrail.
- [ ] Reloading the page restores the conversation.
- [ ] A research question returns an answer with sources.
- [ ] In Supabase, `turns` has one row per message, each with a `trace`.
- [ ] The address header cannot be forged. This should print `201` ten times
      and then `429`; if it prints `201` every time, the proxy is passing the
      client's own header through and `CLIENT_IP_HEADER` must be changed:

      ```bash
      for i in $(seq 1 12); do
        curl -s -o /dev/null -w "%{http_code}\n" -X POST \
          -H "X-Real-IP: 203.0.113.$i" https://<api-domain>/api/sessions
      done
      ```

## If it does not come up

These four happened on the first deployment of this project, in this order.

**`/api/config` still says `"mode": "demo"` and `"store": "memory"` after the
keys were added.** Railway saves new variables as *staged changes*. The
running container does not get them until the "Apply changes" banner at the
top of the project canvas is deployed. A redeploy of the old deployment does
not pick them up either. The server log says which variable it did not find.

**The log says `permission denied for table usage_daily` with a hint about
the `anon` role.** `SUPABASE_SECRET_KEY` holds the publishable key
(`sb_publishable_...`) or the legacy `anon` key. Those are meant to be locked
out of these tables. Use the key under *Secret keys* (`sb_secret_...`). While
the wrong key is in place the site still answers, but nothing is stored and
the daily budget resets on every restart.

**The domain does not load for you, but it does for others.** A resolver
that looked the name up before the DNS record existed remembers "no such
name" for up to an hour. A VPN makes this easy to miss, because its resolver
is used on every network you try. Compare the two answers:

```bash
nslookup code.example.com 8.8.8.8   # a public resolver
nslookup code.example.com           # the one you are using
```

If the first finds it and the second does not, the record is correct. Turn
the VPN off, flush the local cache, or wait.

**Vercel shows "DNS Change Recommended" next to "Valid Configuration".** The
site works. Vercel now prefers a project-specific CNAME target over
`<project>.vercel.app`; "View DNS configuration" shows it. Edit the existing
record's value in place: deleting and re-adding it reopens the gap above.

## Operating it

**Publish a research answer** after reading it and checking its sources:

```sql
select id, question, created_at from research_answers where not is_public order by created_at desc;
update research_answers set is_public = true where id = '<id>';
```

**See what the agent did:**

```sql
-- recent turns with their cost and any guardrails that fired
select created_at, mode, rounds, cost_usd, flags, left(input, 60) as input
from turns order by created_at desc limit 20;

-- every tool call of one turn, in order
select e->'tool'->>'name' as tool, e->'tool'->>'is_error' as failed, e->'tool'->'input' as input
from turns, jsonb_array_elements(trace) e
where id = <turn id> and e->>'type' = 'tool_call';

-- what the limits are stopping
select kind, count(*) from guardrail_events where created_at > now() - interval '7 days' group by 1 order by 2 desc;

-- spend per day
select * from usage_daily order by day desc limit 14;
```

**Change a limit:** set the environment variable on Railway (the list is in
`.env.example`); the service restarts with the new value.

**Rotate a key:** create the new key, update the Railway variable, then
revoke the old one. The site needs no change.

**Run the live evals:** `make evals-live` locally, or the "Live evals"
workflow in GitHub Actions after adding `ANTHROPIC_API_KEY` as a repository
secret. Commit the updated `web/src/generated/evals.json` to show the
results on the site.

## Testing the database layer locally

The integration tests run the store and the access rules against a real
Postgres behind PostgREST, which is what a Supabase project is:

```bash
# with the Supabase CLI
supabase start && supabase db reset
export SUPABASE_TEST_URL=http://127.0.0.1:54321
export SUPABASE_TEST_SECRET_KEY=<service_role key printed by `supabase start`>
export SUPABASE_TEST_PUBLISHABLE_KEY=<anon key printed by `supabase start`>
make test-integration
```

CI does the same with a plain Postgres service container; see the
`integration` job in `.github/workflows/ci.yml` and the two helper scripts in
`scripts/`.
