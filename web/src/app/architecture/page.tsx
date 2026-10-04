import type { Metadata } from "next";
import { CodeBlock } from "@/components/Code";
import agent from "@/generated/agent.json";
import { site } from "@/lib/site";

export const metadata: Metadata = {
  title: "Architecture",
  description: "How a message flows through the system, where each limit lives, what is stored, and the decisions behind it.",
};

function SystemDiagram() {
  return (
    <div className="ld-scroll">
      <svg
        className="ld arch"
        viewBox="0 0 760 312"
        role="img"
        aria-label="The browser loads pages from Vercel and talks to a Go API on Railway. The API runs checks, the agent loop, and tools inside a per-session sandbox. It streams the conversation to the Claude API and stores sessions and traces in Supabase."
      >
        <defs>
          <marker id="arch-arrow" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="7" markerHeight="7" orient="auto-start-reverse">
            <path d="M0 0 L10 5 L0 10 z" style={{ fill: "var(--ink-2)" }} />
          </marker>
        </defs>

        <rect x="16" y="20" width="150" height="56" rx="12" className="arch-box" />
        <text x="91" y="45" className="ld-title ld-small">Vercel</text>
        <text x="91" y="63" className="ld-sub">serves the pages</text>

        <rect x="16" y="128" width="150" height="76" rx="14" className="ld-you" />
        <text x="91" y="160" className="ld-title">Your browser</text>
        <text x="91" y="180" className="ld-sub">this site</text>

        <line x1="91" y1="126" x2="91" y2="80" className="ld-line" markerEnd="url(#arch-arrow)" />
        <text x="99" y="107" className="ld-label ld-start">pages</text>

        <rect x="252" y="36" width="272" height="240" rx="18" className="ld-program" />
        <text x="388" y="64" className="ld-title">Go API on Railway</text>
        <rect x="276" y="82" width="224" height="40" rx="10" className="arch-stage" />
        <text x="388" y="107" className="ld-chip">1 · checks, limits, budget</text>
        <rect x="276" y="132" width="224" height="40" rx="10" className="arch-stage arch-stage-claude" />
        <text x="388" y="157" className="ld-chip">2 · the agent loop</text>
        <rect x="276" y="182" width="224" height="40" rx="10" className="arch-stage arch-stage-tool" />
        <text x="388" y="207" className="ld-chip">3 · tools, inside a sandbox</text>
        <text x="388" y="250" className="ld-sub">one in-memory workspace per session</text>

        <line x1="168" y1="152" x2="248" y2="152" className="ld-line" markerEnd="url(#arch-arrow)" />
        <text x="208" y="143" className="ld-label">message</text>
        <line x1="250" y1="180" x2="170" y2="180" className="ld-line" markerEnd="url(#arch-arrow)" />
        <text x="208" y="197" className="ld-label">live events</text>

        <rect x="610" y="56" width="134" height="68" rx="14" className="ld-claude" />
        <text x="677" y="86" className="ld-title">Claude API</text>
        <text x="677" y="105" className="ld-sub">model + web search</text>
        <line x1="526" y1="80" x2="606" y2="80" className="ld-line" markerEnd="url(#arch-arrow)" />
        <text x="566" y="71" className="ld-label">conversation</text>
        <line x1="608" y1="104" x2="528" y2="104" className="ld-line" markerEnd="url(#arch-arrow)" />
        <text x="566" y="121" className="ld-label">streamed reply</text>

        <rect x="610" y="188" width="134" height="68" rx="14" className="ld-tool arch-db" />
        <text x="677" y="218" className="ld-title">Supabase</text>
        <text x="677" y="237" className="ld-sub">sessions, traces</text>
        <line x1="526" y1="222" x2="606" y2="222" className="ld-line" markerEnd="url(#arch-arrow)" />
        <text x="566" y="213" className="ld-label">after the reply</text>
      </svg>
    </div>
  );
}

const LIFECYCLE = [
  { title: "Identify the session", body: "The path names a session; the Authorization header carries its token. Only a SHA-256 of the token is stored, compared in constant time." },
  { title: "Validate the message", body: "Size limit, control characters stripped, anything shaped like a credential replaced. A rejected message never reaches the model and does not use up a turn." },
  { title: "Check the limits", body: "Session turn count and context size, then the global daily budget and concurrency, then the per-visitor rate limits. Each refusal is an ordinary HTTP error with a reason the UI can show." },
  { title: "Open the stream", body: "From here the response is server-sent events. Every step the agent takes is sent the moment it happens." },
  { title: "Run the loop", body: "Send the conversation, run any tools Claude asks for inside the session's sandbox, send the results back, repeat. Bounded by a round limit and a timeout." },
  { title: "Account for it", body: "Token counts become an estimated cost, added to today's budget whether or not the turn succeeded. A call cancelled part-way is charged for what it had used." },
  { title: "Persist, after replying", body: "Session state, the full trace, usage and guardrail events are written to Supabase in the background. A failed write is logged and never fails the visitor's request." },
];

const THREATS = [
  { risk: "The model is talked into reading or writing files it should not", control: "It can't. Paths are validated and resolved inside a per-session in-memory workspace. There is no host filesystem behind the web tools.", where: "internal/workspace" },
  { risk: "The model writes harmful code and runs it", control: "There is no tool that executes anything. The Run button executes JavaScript in the visitor's own browser, in a worker that is killed after three seconds and can only connect to this site and its API.", where: "web/src/lib/runner.ts" },
  { risk: "A file or web page contains instructions aimed at the model", control: "Tool results are sent as tool results, the system prompt marks them as data, and the model's reach is three sandboxed tools. A successful injection can change files in the attacker's own sandbox.", where: "internal/agent/prompt.go" },
  { risk: "Someone runs up the bill", control: "Output-token cap, round cap, per-session and per-visitor limits, a global daily budget in dollars, a concurrency cap.", where: "internal/server/chat.go" },
  { risk: "Rate limits dodged by forging an address header", control: "No header is trusted by default. The operator names the one the platform's proxy sets; anything a client could have typed is ignored. IPv6 callers are limited per /64 network.", where: "internal/server/server.go" },
  { risk: "A browser that stops reading holds a turn open", control: "Every write to the stream has a deadline. When one fails the turn is cancelled and its slot is freed.", where: "internal/server/chat.go" },
  { risk: "One visitor reads another's conversation", control: "Sessions are addressed by a random ID and opened with a 256-bit token. The database denies all access to the public keys except published research and eval summaries.", where: "supabase/migrations" },
  { risk: "Credentials end up in the model context, logs or database", control: "Credential-shaped strings are replaced in visitor messages and file-tool results before the model sees them, and in replies as they stream and before they are stored. Not covered: text the model itself writes into a file, and web search results. On disk, the terminal agent also refuses .env, key files and .git.", where: "internal/guardrails/redact.go" },
  { risk: "An unreviewed model answer is published as fact", control: "Research answers are stored with is_public = false. Only a person can change that.", where: "internal/server/chat.go" },
];

const DECISIONS = [
  { n: "0001", title: "Security comes from what the agent can reach, not from what it is told", body: "Prompt-level rules are treated as helpful and unreliable. Every property that matters is enforced in Go below the model: path confinement, quotas, limits." },
  { n: "0002", title: "An in-memory workspace per visitor instead of containers", body: "Three tools over small text files do not need a container per session. A map with quotas gives full isolation, starts instantly, and costs nothing when idle. The trade-off is no code execution on the server, which is a feature here." },
  { n: "0003", title: "Test against a fake API over the wire, not a mocked interface", body: "The fake speaks the real Messages API including streaming, so the SDK, the stream accumulator and the loop are all exercised as shipped. The same fake drives demo mode." },
  { n: "0004", title: "Talk to Supabase over REST with no database driver", body: "The server needs a handful of inserts and two reads. PostgREST over HTTPS means one fewer dependency and no connection pool to size." },
  { n: "0005", title: "Heuristic input filters record; they do not block", body: "Phrase matching for prompt injection stops honest questions and does not stop attackers. Matches are logged and shown in the trace. Hard limits do the stopping." },
];

const TABLES = [
  { name: "sessions", holds: "Conversation, workspace files, transcript. Token hash and address hash, never the raw values.", access: "server only" },
  { name: "turns", holds: "One row per message: input, reply, tokens, cost, latency, guardrails fired, and the full event trace.", access: "server only" },
  { name: "research_answers", holds: "Question, answer, sources, and an is_public flag set by a person.", access: "public can read published rows" },
  { name: "usage_daily", holds: "Running totals per UTC day. Read at startup so a restart does not reset the budget.", access: "server only" },
  { name: "guardrail_events", holds: "Every time a limit or check intervened.", access: "server only" },
  { name: "eval_runs", holds: "History of eval suite runs.", access: "public can read" },
];

export default function ArchitecturePage() {
  const l = agent.limits;
  const limits: [string, string, string][] = [
    ["Model", l.model, "AGENT_MODEL"],
    ["Message length", `${l.max_input_chars.toLocaleString("en-US")} characters`, "MAX_INPUT_CHARS"],
    ["Reply length", `${l.max_output_tokens.toLocaleString("en-US")} tokens per model call`, "MAX_OUTPUT_TOKENS"],
    ["Rounds per message", `${l.max_rounds}; the last one must be a text answer`, "MAX_ROUNDS"],
    ["Time per message", `${l.turn_timeout_seconds} seconds`, "TURN_TIMEOUT"],
    ["Messages per session", String(l.max_turns_per_session), "MAX_TURNS_PER_SESSION"],
    ["Conversation size", `${l.max_context_tokens.toLocaleString("en-US")} tokens, then the session must be reset`, "MAX_CONTEXT_TOKENS"],
    ["Messages per visitor", `${l.turns_per_minute} a minute, ${l.turns_per_day} a day`, "TURNS_PER_MINUTE, TURNS_PER_DAY"],
    ["Research questions per visitor", `${l.research_per_day} a day, ${l.web_search_max_uses} searches per model call`, "RESEARCH_PER_DAY, WEB_SEARCH_MAX_USES"],
    ["New sessions per visitor", `${l.sessions_per_hour} an hour`, "SESSIONS_PER_HOUR"],
    ["API calls per visitor, of any kind", `${l.requests_per_minute} a minute`, "REQUESTS_PER_MINUTE"],
    ["Turns running at once", String(l.max_concurrent_turns), "MAX_CONCURRENT_TURNS"],
    ["Daily budget, all visitors", `$${l.daily_budget_usd.toFixed(2)}; when spent, the model is not called until midnight UTC`, "DAILY_BUDGET_USD"],
    ["Workspace", `${l.workspace_max_files} files, ${l.workspace_max_file_kb} KB per file, ${l.workspace_max_kb} KB in total`, "set in code"],
  ];

  return (
    <div className="wrap">
      <div className="page-head">
        <p className="eyebrow">Under the hood</p>
        <h1>Architecture</h1>
        <p>
          The agent itself is small. Most of this system is the part that decides what the agent is allowed to do, how much it may cost, and how anyone would know what it did. This page walks through that part.
        </p>
      </div>

      <section className="section-plain">
        <h2>The system</h2>
        <figure className="split-figure">
          <SystemDiagram />
          <figcaption>
            Three hosted pieces and one API. The browser never talks to Supabase or to Claude directly, and no API key ever reaches it.
          </figcaption>
        </figure>
      </section>

      <section className="section-plain">
        <h2>One message, start to finish</h2>
        <ol className="lifecycle">
          {LIFECYCLE.map((s) => (
            <li key={s.title}>
              <h3>{s.title}</h3>
              <p>{s.body}</p>
            </li>
          ))}
        </ol>
        <p className="figure-note">
          The order is the order of the code in <code>internal/server/chat.go</code>.
        </p>
      </section>

      <section className="section-plain" id="limits">
        <h2>Limits</h2>
        <p>
          These are the values the public demo runs with, exported from the Go defaults when this page was built. Each can be tightened with an environment variable, without a rebuild.
        </p>
        <div className="table-scroll">
          <table className="table">
            <thead>
              <tr>
                <th scope="col">Limit</th>
                <th scope="col">Value</th>
                <th scope="col">Setting</th>
              </tr>
            </thead>
            <tbody>
              {limits.map(([name, value, env]) => (
                <tr key={name}>
                  <th scope="row">{name}</th>
                  <td>{value}</td>
                  <td>
                    <code>{env}</code>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </section>

      <section className="section-plain" id="threats">
        <h2>What could go wrong, and what stops it</h2>
        <div className="table-scroll">
          <table className="table">
            <thead>
              <tr>
                <th scope="col">Risk</th>
                <th scope="col">What stops it</th>
                <th scope="col">Where</th>
              </tr>
            </thead>
            <tbody>
              {THREATS.map((t) => (
                <tr key={t.risk}>
                  <th scope="row">{t.risk}</th>
                  <td>{t.control}</td>
                  <td>
                    <code>{t.where}</code>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </section>

      <section className="section-plain" id="tools">
        <h2>Exactly what the model is given</h2>
        <p>The system prompt and the tool menu below are exported from the Go source, so this is the text in use, not a description of it.</p>
        <div className="given">
          <CodeBlock code={agent.system_prompt_code} title="System prompt, code mode" />
          <div className="given-tools">
            {agent.tools.map((t) => (
              <CodeBlock key={t.name} code={JSON.stringify({ name: t.name, description: t.description, input_schema: t.schema }, null, 2)} title={`Tool: ${t.name}${t.mutates ? " (changes files; asks for approval in the terminal)" : ""}`} />
            ))}
          </div>
        </div>
      </section>

      <section className="section-plain" id="data">
        <h2>What is stored</h2>
        <p>
          Row level security is on for every table. The server uses a secret key that is only in its environment. The publishable key that ships with any Supabase project can read published research answers and eval summaries, and nothing else. Visitor data is deleted after 30 days by a scheduled job.
        </p>
        <div className="table-scroll">
          <table className="table">
            <thead>
              <tr>
                <th scope="col">Table</th>
                <th scope="col">Holds</th>
                <th scope="col">Who can read it</th>
              </tr>
            </thead>
            <tbody>
              {TABLES.map((t) => (
                <tr key={t.name}>
                  <th scope="row">
                    <code>{t.name}</code>
                  </th>
                  <td>{t.holds}</td>
                  <td>{t.access}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </section>

      <section className="section-plain" id="decisions">
        <h2>Decisions</h2>
        <p>
          The reasoning behind the main choices, in short. The full records, with the alternatives that were rejected, are in{" "}
          <a href={`${site.repo}/tree/main/docs/adr`} target="_blank" rel="noopener noreferrer">
            docs/adr
          </a>
          .
        </p>
        <dl className="decisions">
          {DECISIONS.map((d) => (
            <div key={d.n}>
              <dt>
                <span className="decision-n">ADR {d.n}</span>
                {d.title}
              </dt>
              <dd>{d.body}</dd>
            </div>
          ))}
        </dl>
      </section>

      <section className="section-plain" id="limits-of-this">
        <h2>What this does not do</h2>
        <ul className="plain-list">
          <li>Rate limits and live sessions are held in the memory of one server process. Running several instances would need those counters moved to Postgres or Redis; the interfaces are shaped for that, the work is not done.</li>
          <li>Cost is estimated from list prices and reported token counts. It is close enough to enforce a budget and is not an invoice.</li>
          <li>The budget is checked before a turn and charged after it, so a few turns running at once can overshoot by the cost of those turns.</li>
          <li>Credential redaction recognises common key formats. A secret with no recognisable shape will pass through, and text the model writes into a file is not redacted.</li>
          <li>Visitors are anonymous. There are no accounts, so limits are per network address, which shared networks share.</li>
          <li>Demo mode is a scripted stand-in that recognises a handful of requests. It exists so the loop can be seen working without an API key, and the page says so whenever it is active.</li>
        </ul>
      </section>
    </div>
  );
}
