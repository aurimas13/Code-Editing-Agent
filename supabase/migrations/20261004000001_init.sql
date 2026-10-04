-- Code-Editing-Agent: sessions, traces, research answers, usage, evals.
--
-- Access model
--   The Go server is the only writer. It connects with the project's secret
--   key, which bypasses row level security. Browsers never talk to this
--   database. Row level security is enabled on every table anyway, so the
--   publishable key that ships in any Supabase project can read exactly two
--   things: research answers a person has published, and eval run summaries.

-- ---------------------------------------------------------------------------
-- Tables
-- ---------------------------------------------------------------------------

create table public.sessions (
  id              uuid primary key,
  token_hash      text        not null,           -- SHA-256 of the bearer token; the token itself is never stored
  ip_hash         text        not null,           -- keyed hash of the client address; raw addresses are never stored
  model           text        not null,
  turn_count      integer     not null default 0,
  state           jsonb       not null default '{}'::jsonb, -- conversation, workspace files, transcript
  created_at      timestamptz not null default now(),
  last_active_at  timestamptz not null default now()
);
comment on table public.sessions is 'One anonymous visitor session: its conversation and sandbox workspace.';

create table public.turns (
  id             bigint generated always as identity primary key,
  session_id     uuid        not null references public.sessions (id) on delete cascade,
  idx            integer     not null,
  mode           text        not null check (mode in ('code', 'research')),
  model          text        not null,
  input          text        not null,
  reply          text        not null default '',
  stop_reason    text,
  rounds         integer     not null default 0,
  input_tokens   bigint      not null default 0,
  output_tokens  bigint      not null default 0,
  web_searches   bigint      not null default 0,
  cost_usd       numeric(12, 6) not null default 0,
  latency_ms     bigint      not null default 0,
  flags          text[]      not null default '{}',   -- guardrails that fired during the turn
  error          text,
  trace          jsonb       not null default '[]'::jsonb, -- every event of the turn, in order
  created_at     timestamptz not null default now(),
  unique (session_id, idx)
);
comment on table public.turns is 'One user message and the full trace of what the agent did in response.';

create table public.research_answers (
  id          uuid primary key default gen_random_uuid(),
  session_id  uuid references public.sessions (id) on delete set null,
  question    text        not null,
  answer      text        not null,
  sources     jsonb       not null default '[]'::jsonb,
  model       text        not null,
  is_public   boolean     not null default false,  -- set by a person after review; never by the agent
  created_at  timestamptz not null default now()
);
comment on table public.research_answers is 'Questions answered with web search. Hidden until a person publishes them.';

create table public.usage_daily (
  day            date primary key,
  turns          bigint not null default 0,
  input_tokens   bigint not null default 0,
  output_tokens  bigint not null default 0,
  web_searches   bigint not null default 0,
  cost_usd       numeric(12, 6) not null default 0
);
comment on table public.usage_daily is 'Daily totals across all visitors. The server reads this at startup to resume the spending cap.';

create table public.guardrail_events (
  id          bigint generated always as identity primary key,
  session_id  uuid references public.sessions (id) on delete cascade,
  ip_hash     text        not null,
  kind        text        not null,
  detail      text,
  created_at  timestamptz not null default now()
);
comment on table public.guardrail_events is 'Every time a limit or check intervened, for abuse review.';

create table public.eval_runs (
  id          uuid primary key default gen_random_uuid(),
  suite       text        not null,
  model       text,
  git_sha     text,
  passed      integer     not null,
  failed      integer     not null,
  total       integer     not null,
  report      jsonb       not null,
  created_at  timestamptz not null default now()
);
comment on table public.eval_runs is 'History of eval suite runs.';

-- ---------------------------------------------------------------------------
-- Indexes
-- ---------------------------------------------------------------------------

create index sessions_last_active_idx        on public.sessions (last_active_at);
create index research_public_recent_idx      on public.research_answers (created_at desc) where is_public;
create index research_session_idx            on public.research_answers (session_id);
create index guardrail_events_created_idx    on public.guardrail_events (created_at);
create index guardrail_events_session_idx    on public.guardrail_events (session_id);
create index eval_runs_created_idx           on public.eval_runs (created_at desc);

-- ---------------------------------------------------------------------------
-- Row level security
-- ---------------------------------------------------------------------------

alter table public.sessions          enable row level security;
alter table public.turns             enable row level security;
alter table public.research_answers  enable row level security;
alter table public.usage_daily       enable row level security;
alter table public.guardrail_events  enable row level security;
alter table public.eval_runs         enable row level security;

-- Start from nothing for the public roles, then grant back the minimum.
revoke all on public.sessions, public.turns, public.research_answers,
              public.usage_daily, public.guardrail_events, public.eval_runs
  from anon, authenticated;

-- Published research: only the columns a reader needs. session_id stays private.
grant select (id, question, answer, sources, model, created_at)
  on public.research_answers to anon, authenticated;

create policy "Published answers are readable by anyone"
  on public.research_answers for select
  to anon, authenticated
  using (is_public);

-- Eval summaries are public by design: they are the evidence for the claims on the site.
grant select on public.eval_runs to anon, authenticated;

create policy "Eval runs are readable by anyone"
  on public.eval_runs for select
  to anon, authenticated
  using (true);

-- sessions, turns, usage_daily, guardrail_events: no policies, so no access
-- for anon or authenticated. Only the server's secret key can reach them.

-- ---------------------------------------------------------------------------
-- Functions
-- ---------------------------------------------------------------------------

-- Adds to today's totals in one atomic statement, so concurrent turns cannot
-- lose each other's updates the way read-modify-write from the server could.
create function public.add_usage(
  p_turns bigint,
  p_input_tokens bigint,
  p_output_tokens bigint,
  p_web_searches bigint,
  p_cost_usd numeric
) returns void
language sql
security invoker
set search_path = ''
as $$
  insert into public.usage_daily as u (day, turns, input_tokens, output_tokens, web_searches, cost_usd)
  values ((now() at time zone 'utc')::date, p_turns, p_input_tokens, p_output_tokens, p_web_searches, p_cost_usd)
  on conflict (day) do update set
    turns         = u.turns         + excluded.turns,
    input_tokens  = u.input_tokens  + excluded.input_tokens,
    output_tokens = u.output_tokens + excluded.output_tokens,
    web_searches  = u.web_searches  + excluded.web_searches,
    cost_usd      = u.cost_usd      + excluded.cost_usd;
$$;

-- Deletes visitor data past its retention period. Published research and
-- eval history are kept. Turns and guardrail events go with their session.
create function public.purge_expired(retention interval default interval '30 days')
returns void
language sql
security invoker
set search_path = ''
as $$
  delete from public.sessions          where last_active_at < now() - retention;
  delete from public.guardrail_events  where created_at     < now() - retention;
  delete from public.research_answers  where not is_public and created_at < now() - retention;
$$;

revoke execute on function public.add_usage(bigint, bigint, bigint, bigint, numeric) from public, anon, authenticated;
revoke execute on function public.purge_expired(interval)                            from public, anon, authenticated;
grant  execute on function public.add_usage(bigint, bigint, bigint, bigint, numeric) to service_role;
grant  execute on function public.purge_expired(interval)                            to service_role;
