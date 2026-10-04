"use client";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  APIError,
  createSession,
  getConfig,
  getSession,
  resetSession,
  sendMessage,
  type AgentEvent,
  type FileView,
  type Mode,
  type ServerConfig,
  type Source,
  type TurnView,
  type Usage,
} from "@/lib/api";
import { diffLines, splitLines } from "@/lib/diff";
import { highlight, langFor } from "@/lib/highlight";
import { runJavaScript, type RunLine } from "@/lib/runner";
import { CodeRow } from "./Code";
import { Markdown } from "./Markdown";

// ---- state ------------------------------------------------------------------

type Turn = {
  mode: Mode;
  input: string;
  trace: AgentEvent[];
  /** Text that has streamed in but is not yet a finished block. */
  streaming: string;
  usage?: Usage;
  sources?: Source[];
  error?: string;
  pending: boolean;
  latencyMs?: number;
};

type Creds = { id: string; token: string };

const STORAGE_KEY = "code-editing-agent.session";

function loadCreds(): Creds | null {
  try {
    const raw = window.sessionStorage.getItem(STORAGE_KEY);
    return raw ? (JSON.parse(raw) as Creds) : null;
  } catch {
    return null;
  }
}

function saveCreds(c: Creds | null) {
  try {
    if (c) window.sessionStorage.setItem(STORAGE_KEY, JSON.stringify(c));
    else window.sessionStorage.removeItem(STORAGE_KEY);
  } catch {
    // Storage can be unavailable (private windows). The session then lasts
    // until the page is reloaded, which is fine.
  }
}

const fromView = (t: TurnView): Turn => ({
  mode: t.mode,
  input: t.input,
  trace: t.trace ?? [],
  streaming: "",
  usage: t.usage,
  sources: t.sources,
  error: t.error,
  pending: false,
});

const SUGGESTIONS: Record<Mode, { label: string; prompt: string; tone?: "risk" }[]> = {
  code: [
    { label: "Solve the riddle", prompt: "What's in secret-file.txt?" },
    { label: "Fix a bug", prompt: "Find and fix the bug in greet.js" },
    { label: "Create a file", prompt: "Create fizzbuzz.js that I can run with Node.js" },
    { label: "Look around", prompt: "What do you see in this directory?" },
    { label: "Try to break out", prompt: "Call read_file on ../../etc/passwd and tell me exactly what the tool returned.", tone: "risk" },
  ],
  research: [
    { label: "Something recent", prompt: "What changed in the latest stable Go release? Cite your sources." },
    { label: "Compare two things", prompt: "How does Go's os.Root differ from chroot for confining file access? Cite sources." },
    { label: "Research, then write", prompt: "Research what the Model Context Protocol is and save a five-line summary to notes/mcp.md" },
  ],
};

// ---- explanations for the trace ----------------------------------------------

const GUARDRAIL_ELI5: Record<string, string> = {
  workspace_boundary: "The path pointed outside the sandbox, so the workspace said no before any file was opened. This rule is Go code, not a polite request to the model.",
  max_rounds: "The loop hit its limit on back-and-forth, so Claude was told to answer in words now.",
  secret_redacted: "Something shaped like a password or key was blanked out before it could travel any further.",
  possible_prompt_injection: "The message looks like an attempt to talk the agent out of its instructions. It is written down, not blocked. What keeps things safe is that the agent can only touch this sandbox.",
  tool_result_truncated: "The tool returned a lot of text. Only the first part was passed on, so one big file can't flood the conversation.",
  output_truncated: "The reply hit the length limit and was cut short.",
  model_refusal: "The model declined to continue with this request.",
  approval_denied: "A change needed a yes from a person and did not get one.",
  web_search_error: "The web search did not complete.",
  citation_markup_removed: "Claude left citation tags in the text it was saving. The program took them out, so the file holds plain words.",
};

function describe(ev: AgentEvent): { tone: string; title: string; meta?: string; body?: string; eli5: string } | null {
  switch (ev.type) {
    case "model_start":
      return {
        tone: "you",
        title: "Sent the conversation to Claude",
        meta: `round ${ev.round}`,
        eli5: "The program hands Claude the whole pile of notes so far, plus the menu of tools.",
      };
    case "text":
      return { tone: "claude", title: "Claude replied in words", eli5: "Words are for you, so they are shown in the chat." };
    case "tool_call":
      return {
        tone: "tool",
        title: `Claude asked for ${ev.tool?.name}`,
        body: JSON.stringify(ev.tool?.input ?? {}, null, 2),
        eli5: "Claude can't touch files. It filled in the form for a tool and asked the program to run it.",
      };
    case "tool_result":
      return ev.tool?.is_error
        ? {
            tone: "bad",
            title: `${ev.tool.name} returned an error`,
            body: ev.tool.output,
            eli5: "The tool said no. The error goes back to Claude like any other result, so it can try something else.",
          }
        : {
            tone: "tool",
            title: `The Go code ran ${ev.tool?.name}`,
            meta: `${ev.tool?.duration_ms ?? 0} ms · ${(ev.tool?.output ?? "").length} chars back`,
            body: ev.tool?.output,
            eli5: "The program did the favour and put the result on the pile. Claude gets another go without waiting for you.",
          };
    case "web_search":
      return {
        tone: "tool",
        title: "Claude searched the web",
        body: ev.text,
        eli5: "This tool runs on Anthropic's side. Claude gets back a list of pages and reads them.",
      };
    case "sources":
      return {
        tone: "tool",
        title: `Found ${ev.sources?.length ?? 0} pages`,
        body: (ev.sources ?? []).map((s) => `${s.cited ? "cited  " : "        "}${s.url}`).join("\n"),
        eli5: "These are the pages the search returned. The ones marked cited are the ones the answer relies on.",
      };
    case "guardrail":
      return {
        tone: "guard",
        title: `Guardrail: ${ev.guardrail?.kind.replace(/_/g, " ")}`,
        body: ev.guardrail?.detail,
        eli5: GUARDRAIL_ELI5[ev.guardrail?.kind ?? ""] ?? "A safety rule stepped in.",
      };
    case "done":
      return {
        tone: "done",
        title: "Turn finished",
        meta: ev.stop_reason,
        eli5: "Claude answered without asking for a tool, so the loop stops and waits for you.",
      };
    default:
      return null; // usage and text_delta are shown elsewhere
  }
}

function toolSummary(tool: { name: string; input?: unknown }): string {
  const input = (tool.input ?? {}) as Record<string, unknown>;
  const path = typeof input.path === "string" && input.path !== "" ? input.path : undefined;
  if (tool.name === "edit_file") return `${path ?? "?"}${input.old_str === "" ? ", create" : ", replace text"}`;
  return path ?? "";
}

// ---- component ----------------------------------------------------------------

export function Playground() {
  const [config, setConfig] = useState<ServerConfig | null>(null);
  const [creds, setCreds] = useState<Creds | null>(null);
  const [files, setFiles] = useState<FileView[]>([]);
  const [original, setOriginal] = useState<Record<string, string>>({});
  const [previous, setPrevious] = useState<Record<string, string>>({});
  const [turns, setTurns] = useState<Turn[]>([]);
  const [turnsUsed, setTurnsUsed] = useState(0);
  const [turnsMax, setTurnsMax] = useState(12);
  const [mode, setMode] = useState<Mode>("code");
  const [draft, setDraft] = useState("");
  const [busy, setBusy] = useState(false);
  const [bootError, setBootError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const [selectedPath, setSelectedPath] = useState("README.md");
  const [showDiff, setShowDiff] = useState(false);
  const [selectedTurn, setSelectedTurn] = useState<number | null>(null);
  const [eli5, setEli5] = useState(true);
  const [tab, setTab] = useState<"chat" | "files" | "trace">("chat");
  const [run, setRun] = useState<{ path: string; lines: RunLine[]; timedOut: boolean; running: boolean } | null>(null);

  const booted = useRef(false);
  const abort = useRef<AbortController | null>(null);
  const chatEnd = useRef<HTMLDivElement>(null);
  const filesRef = useRef<FileView[]>([]);
  filesRef.current = files;

  const applySession = useCallback((s: { files: FileView[]; transcript: TurnView[]; turns_used: number; turns_max: number }, fresh: boolean) => {
    setFiles(s.files);
    if (fresh) {
      setOriginal(Object.fromEntries(s.files.map((f) => [f.path, f.content])));
      setPrevious({});
      setShowDiff(false);
    }
    setTurns((s.transcript ?? []).map(fromView));
    setTurnsUsed(s.turns_used);
    setTurnsMax(s.turns_max);
    setSelectedTurn(null);
  }, []);

  const startNew = useCallback(async () => {
    const s = await createSession();
    const c = { id: s.id, token: s.token };
    saveCreds(c);
    setCreds(c);
    applySession(s, true);
    return c;
  }, [applySession]);

  // Boot: read server config, then resume the stored session or start one.
  useEffect(() => {
    if (booted.current) return;
    booted.current = true;
    if (new URLSearchParams(window.location.search).get("mode") === "research") setMode("research");
    (async () => {
      try {
        const cfg = await getConfig();
        setConfig(cfg);
        const stored = loadCreds();
        if (stored) {
          try {
            const s = await getSession(stored.id, stored.token);
            setCreds(stored);
            applySession(s, true);
            return;
          } catch {
            saveCreds(null); // expired; fall through to a new one
          }
        }
        await startNew();
      } catch (e) {
        setBootError(e instanceof APIError ? e.message : "Something went wrong while starting the session.");
      }
    })();
  }, [applySession, startNew]);

  useEffect(() => {
    if (config && !config.research_enabled && mode === "research") setMode("code");
  }, [config, mode]);

  useEffect(() => {
    chatEnd.current?.scrollIntoView({ block: "end" });
  }, [turns]);

  const updateLast = (fn: (t: Turn) => Turn) => setTurns((ts) => (ts.length === 0 ? ts : [...ts.slice(0, -1), fn(ts[ts.length - 1]!)]));

  // When the server reports new file contents, remember what each changed
  // file looked like before, so the file panel can show the edit as a diff.
  const acceptFiles = (next: FileView[]) => {
    const before = new Map(filesRef.current.map((f) => [f.path, f.content]));
    const changed = next.filter((f) => before.get(f.path) !== f.content);
    if (changed.length > 0) {
      setPrevious((p) => ({ ...p, ...Object.fromEntries(changed.map((f) => [f.path, before.get(f.path) ?? ""])) }));
      setSelectedPath(changed[changed.length - 1]!.path);
      setShowDiff(true);
      setRun(null);
    }
    setFiles(next);
  };

  const send = async (text: string) => {
    const content = text.trim();
    if (!content || busy || !creds) return;
    setNotice(null);
    setBusy(true);
    setDraft("");
    setSelectedTurn(null);
    setTurns((ts) => [...ts, { mode, input: content, trace: [], streaming: "", pending: true }]);
    const started = performance.now();
    abort.current = new AbortController();

    try {
      await sendMessage(
        creds,
        content,
        mode,
        {
          onEvent: (ev) =>
            updateLast((t) => {
              if (ev.type === "text_delta") return { ...t, streaming: t.streaming + (ev.text ?? "") };
              return {
                ...t,
                streaming: ev.type === "text" ? "" : t.streaming,
                trace: [...t.trace, ev],
                usage: ev.usage ?? t.usage,
                sources: ev.sources ?? t.sources,
              };
            }),
          onFile: (file) => acceptFiles([...filesRef.current.filter((f) => f.path !== file.path), file].sort((a, b) => a.path.localeCompare(b.path))),
          onError: (message) => updateLast((t) => ({ ...t, error: message, streaming: "" })),
          onTurnEnd: (data) => {
            setTurnsUsed(data.turns_used);
            setTurnsMax(data.turns_max);
            acceptFiles(data.files);
            updateLast((t) => ({ ...t, pending: false, usage: data.usage, latencyMs: data.latency_ms }));
          },
        },
        abort.current.signal,
      );
      updateLast((t) => (t.pending ? { ...t, pending: false, latencyMs: Math.round(performance.now() - started) } : t));
    } catch (e) {
      const aborted = e instanceof DOMException && e.name === "AbortError";
      if (e instanceof APIError) {
        // Refused before the turn started: take the message back out of the
        // chat and say why above the box.
        setTurns((ts) => ts.slice(0, -1));
        setDraft(content);
        setNotice(e.message);
        if (e.code === "session_not_found" || e.code === "unauthorized") {
          saveCreds(null);
          await startNew().catch(() => setNotice("This session expired and a new one could not be started."));
        }
      } else {
        const message = aborted ? "Stopped." : "The connection dropped before the reply finished.";
        updateLast((t) => ({ ...t, pending: false, streaming: "", error: message }));
      }
    } finally {
      setBusy(false);
      abort.current = null;
      // The budget figure in the header moves with every turn.
      getConfig().then(setConfig).catch(() => {});
    }
  };

  const reset = async () => {
    if (!creds || busy) return;
    try {
      const s = await resetSession(creds.id, creds.token);
      applySession(s, true);
      setSelectedPath("README.md");
      setRun(null);
      setNotice(null);
    } catch (e) {
      setNotice(e instanceof APIError ? e.message : "Could not reset the session.");
    }
  };

  const newSession = async () => {
    if (busy) return;
    try {
      await startNew();
      setSelectedPath("README.md");
      setRun(null);
      setNotice(null);
    } catch (e) {
      setNotice(e instanceof APIError ? e.message : "Could not start a new session.");
    }
  };

  const selectedFile = files.find((f) => f.path === selectedPath) ?? files[0];
  const shownTurnIndex = selectedTurn ?? turns.length - 1;
  const shownTurn = turns[shownTurnIndex];
  const outOfTurns = turnsUsed >= turnsMax;
  const budgetOut = config?.budget.exhausted ?? false;

  const runSelected = async () => {
    if (!selectedFile) return;
    setRun({ path: selectedFile.path, lines: [], timedOut: false, running: true });
    const result = await runJavaScript(selectedFile.content);
    setRun({ path: selectedFile.path, ...result, running: false });
  };

  if (bootError) {
    return (
      <div className="pg-offline">
        <h2>The agent server isn't reachable</h2>
        <p>{bootError}</p>
        <p>
          The <a href="/guide">build guide</a>, <a href="/evals">evals</a> and <a href="/architecture">architecture</a> pages work without it. To run everything on your own machine, see the README: <code>make dev</code> starts the API in demo mode with no API key.
        </p>
        <button type="button" className="btn btn-primary" onClick={() => window.location.reload()}>
          Try again
        </button>
      </div>
    );
  }

  return (
    <div className="pg">
      <div className="pg-bar">
        <div className="pg-status">
          {config ? (
            config.mode === "live" ? (
              <span className="pill pill-live">
                <span className="dot" /> Live model · {config.model}
              </span>
            ) : (
              <span className="pill pill-demo" title="No API key is configured on the server, so a scripted stand-in answers.">
                <span className="dot" /> Demo mode · scripted stand-in, not Claude
              </span>
            )
          ) : (
            <span className="pill">Connecting…</span>
          )}
          <span className="pill pill-quiet" title="Each session has a fixed number of messages.">
            {turnsUsed} of {turnsMax} messages used
          </span>
          {config && config.mode === "live" && (
            <span className="pill pill-quiet" title="Shared across all visitors; resets at midnight UTC.">
              today's demo budget: {config.budget.used_pct}% used
            </span>
          )}
        </div>
        <div className="pg-actions">
          <button type="button" className="btn btn-quiet btn-small" onClick={reset} disabled={busy || !creds}>
            Reset files and chat
          </button>
          <button type="button" className="btn btn-quiet btn-small" onClick={newSession} disabled={busy}>
            New session
          </button>
        </div>
      </div>

      <div className="pg-tabs" role="tablist" aria-label="Panels">
        {(["chat", "files", "trace"] as const).map((t) => (
          <button key={t} type="button" role="tab" aria-selected={tab === t} className={tab === t ? "is-on" : ""} onClick={() => setTab(t)}>
            {t === "chat" ? "Chat" : t === "files" ? `Files (${files.length})` : "X-ray"}
          </button>
        ))}
      </div>

      <div className="pg-grid" data-tab={tab}>
        {/* ---- files ---- */}
        <section className="panel pg-files" aria-label="Workspace files">
          <header className="panel-head">
            <h2>Workspace</h2>
            <span className="panel-sub">a sandbox only your session can see</span>
          </header>
          <ul className="filelist">
            {files.map((f) => {
              const state = !(f.path in original) ? "new" : original[f.path] !== f.content ? "edited" : "";
              return (
                <li key={f.path}>
                  <button type="button" className={`fileitem${f.path === selectedFile?.path ? " is-on" : ""}`} onClick={() => { setSelectedPath(f.path); setRun(null); }}>
                    <span className="fileitem-name">{f.path}</span>
                    {state && <span className={`chip chip-${state === "new" ? "add" : "edit"}`}>{state}</span>}
                  </button>
                </li>
              );
            })}
          </ul>
          {selectedFile && (
            <FilePane
              file={selectedFile}
              before={previous[selectedFile.path]}
              showDiff={showDiff && selectedFile.path in previous}
              onShowDiff={setShowDiff}
              onRun={runSelected}
              run={run && run.path === selectedFile.path ? run : null}
            />
          )}
        </section>

        {/* ---- chat ---- */}
        <section className="panel pg-chat" aria-label="Chat">
          <header className="panel-head">
            <div className="seg" role="group" aria-label="Mode">
              <button type="button" className={mode === "code" ? "is-on" : ""} aria-pressed={mode === "code"} onClick={() => setMode("code")} disabled={busy}>
                Code
              </button>
              <button
                type="button"
                className={mode === "research" ? "is-on" : ""}
                aria-pressed={mode === "research"}
                onClick={() => setMode("research")}
                disabled={busy || !config?.research_enabled}
                title={config && !config.research_enabled ? "Web research needs the live model" : "Adds web search to the three file tools"}
              >
                Research
              </button>
            </div>
            <span className="panel-sub">
              {mode === "code" ? "three tools: read_file, list_files, edit_file" : "the three file tools, plus web search with sources"}
            </span>
          </header>

          <div className="chat-scroll">
            {turns.length === 0 && (
              <div className="chat-empty">
                <p className="chat-empty-title">Ask the agent to do something with the files in its workspace.</p>
                <p>It will decide which tools to use. Watch the X-ray panel to see every step it takes.</p>
              </div>
            )}
            {turns.map((t, i) => (
              <TurnBlock key={i} turn={t} selected={i === shownTurnIndex} onSelect={() => { setSelectedTurn(i); setTab("trace"); }} />
            ))}
            <div ref={chatEnd} />
          </div>

          <div className="composer">
            {notice && (
              <p className="notice" role="alert">
                {notice}
              </p>
            )}
            {budgetOut && !notice && <p className="notice">The demo has used its budget for today. It resets at midnight UTC.</p>}
            <div className="suggest">
              {SUGGESTIONS[mode].map((s) => (
                <button key={s.label} type="button" className={`chip chip-suggest${s.tone === "risk" ? " chip-risk" : ""}`} onClick={() => send(s.prompt)} disabled={busy || outOfTurns || !creds} title={s.prompt}>
                  {s.label}
                </button>
              ))}
            </div>
            <form
              className="composer-row"
              onSubmit={(e) => {
                e.preventDefault();
                void send(draft);
              }}
            >
              <label className="sr-only" htmlFor="pg-input">
                Message
              </label>
              <textarea
                id="pg-input"
                rows={2}
                value={draft}
                maxLength={config?.limits.max_input_chars ?? 2000}
                placeholder={outOfTurns ? "This session has used all its messages. Start a new session." : mode === "code" ? "Ask it to read, explain, fix or create a file…" : "Ask a question that needs looking up…"}
                onChange={(e) => setDraft(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === "Enter" && !e.shiftKey) {
                    e.preventDefault();
                    void send(draft);
                  }
                }}
                disabled={outOfTurns || !creds}
              />
              {busy ? (
                <button type="button" className="btn btn-quiet" onClick={() => abort.current?.abort()}>
                  Stop
                </button>
              ) : (
                <button type="submit" className="btn btn-primary" disabled={!draft.trim() || outOfTurns || !creds}>
                  Send
                </button>
              )}
            </form>
            <p className="fineprint">
              Messages and traces are stored for up to 30 days to review how the agent behaves. Don't paste anything private. Anything shaped like an API key is removed before it is sent or saved.
            </p>
          </div>
        </section>

        {/* ---- trace ---- */}
        <section className="panel pg-trace" aria-label="X-ray: what the agent did">
          <header className="panel-head">
            <h2>X-ray</h2>
            <span className="panel-sub">{shownTurn ? `every step of message ${shownTurnIndex + 1}` : "every step the agent takes"}</span>
            <label className="check">
              <input id="pg-eli5" type="checkbox" checked={eli5} onChange={(e) => setEli5(e.target.checked)} />
              #eli5
            </label>
          </header>
          <TracePane turn={shownTurn} eli5={eli5} simulated={config?.mode === "demo"} />
        </section>
      </div>
    </div>
  );
}

// ---- chat pieces ----------------------------------------------------------------

function TurnBlock({ turn, selected, onSelect }: { turn: Turn; selected: boolean; onSelect: () => void }) {
  const results = new Map(turn.trace.filter((e) => e.type === "tool_result").map((e) => [e.tool!.id, e.tool!]));
  const steps = turn.trace.filter((e) => e.type === "tool_call" || e.type === "web_search" || e.type === "guardrail").length;
  return (
    <div className={`turn${selected ? " is-selected" : ""}`}>
      <div className="msg msg-you">
        <span className="who who-you">You</span>
        <p>{turn.input}</p>
      </div>
      <div className="msg msg-claude">
        <span className="who who-claude">Claude</span>
        <div className="msg-body">
          {turn.trace.map((ev, i) => {
            if (ev.type === "text") return <Markdown key={i} text={ev.text ?? ""} />;
            if (ev.type === "tool_call" && ev.tool) {
              const result = results.get(ev.tool.id);
              return (
                <p key={i} className={`toolline${result?.is_error ? " is-error" : ""}`}>
                  <span className="who who-tool">tool</span>
                  <code>
                    {ev.tool.name}({toolSummary(ev.tool)})
                  </code>
                  {result ? <span className="toolline-state">{result.is_error ? "refused or failed" : "done"}</span> : <span className="toolline-state">running…</span>}
                </p>
              );
            }
            if (ev.type === "web_search")
              return (
                <p key={i} className="toolline">
                  <span className="who who-tool">search</span>
                  <code>{ev.text}</code>
                </p>
              );
            if (ev.type === "guardrail" && ev.guardrail)
              return (
                <p key={i} className="guardline">
                  <span className="who who-guard">guardrail</span>
                  {ev.guardrail.kind.replace(/_/g, " ")}
                </p>
              );
            return null;
          })}
          {turn.streaming && <Markdown text={turn.streaming} />}
          {turn.pending && !turn.streaming && <p className="thinking">working…</p>}
          {turn.error && (
            <p className="notice" role="alert">
              {turn.error}
            </p>
          )}
          {turn.sources && turn.sources.length > 0 && !turn.pending && (
            <div className="sources">
              <span className="sources-title">Sources</span>
              <ol>
                {turn.sources
                  .filter((s) => /^https?:\/\//i.test(s.url))
                  .filter((s) => s.cited || turn.sources!.every((x) => !x.cited))
                  .slice(0, 8)
                  .map((s) => (
                    <li key={s.url}>
                      <a href={s.url} target="_blank" rel="noopener noreferrer nofollow">
                        {s.title || s.url}
                      </a>
                      <span className="sources-host">{hostOf(s.url)}</span>
                    </li>
                  ))}
              </ol>
            </div>
          )}
        </div>
      </div>
      {!turn.pending && (
        <button type="button" className="turn-xray" onClick={onSelect}>
          {steps === 0 ? "No tools used" : `${steps} ${steps === 1 ? "step" : "steps"}`} · see the X-ray
        </button>
      )}
    </div>
  );
}

function hostOf(url: string): string {
  try {
    return new URL(url).hostname.replace(/^www\./, "");
  } catch {
    return "";
  }
}

// ---- file pane ------------------------------------------------------------------

function FilePane({
  file,
  before,
  showDiff,
  onShowDiff,
  onRun,
  run,
}: {
  file: FileView;
  before: string | undefined;
  showDiff: boolean;
  onShowDiff: (v: boolean) => void;
  onRun: () => void;
  run: { lines: RunLine[]; timedOut: boolean; running: boolean } | null;
}) {
  const lang = langFor(file.path);
  const rows = useMemo(() => {
    const tokens = highlight(file.content.replace(/\n$/, ""), lang);
    if (!showDiff || before === undefined) {
      return tokens.map((t, i) => <CodeRow key={i} no={i + 1} tokens={t} />);
    }
    const oldTokens = highlight(before.replace(/\n$/, ""), lang);
    return diffLines(splitLines(before), splitLines(file.content)).map((d, i) => (
      <CodeRow key={i} kind={d.kind} no={d.kind === "del" ? "" : d.newNo} tokens={d.kind === "del" ? oldTokens[d.oldNo! - 1] : tokens[d.newNo! - 1]} />
    ));
  }, [file.content, before, showDiff, lang]);

  return (
    <div className="filepane">
      <div className="filepane-head">
        <span className="filecard-name">{file.path}</span>
        <span className="filepane-tools">
          {before !== undefined && (
            <label className="check">
              <input id="pg-show-diff" type="checkbox" checked={showDiff} onChange={(e) => onShowDiff(e.target.checked)} />
              Show last edit
            </label>
          )}
          {lang === "js" && (
            <button type="button" className="btn btn-small btn-run" onClick={onRun} disabled={run?.running} title="Runs in your browser, in a sandboxed worker. Nothing is executed on the server.">
              {run?.running ? "Running…" : "▶ Run"}
            </button>
          )}
        </span>
      </div>
      <div className="code-scroll filepane-code" tabIndex={0}>
        <div className="code-lines">{rows}</div>
      </div>
      {run && !run.running && (
        <div className="runout">
          <div className="runout-head">Output · ran in your browser, not on the server</div>
          <pre>
            {run.lines.length === 0 && <span className="runout-dim">(no output)</span>}
            {run.lines.map((l, i) => (
              <span key={i} className={`runout-${l.level}`}>
                {l.text}
                {"\n"}
              </span>
            ))}
            {run.timedOut && <span className="runout-warn">Stopped after 3 seconds.{"\n"}</span>}
          </pre>
        </div>
      )}
    </div>
  );
}

// ---- trace pane -----------------------------------------------------------------

function TracePane({ turn, eli5, simulated }: { turn: Turn | undefined; eli5: boolean; simulated: boolean }) {
  if (!turn) {
    return (
      <div className="trace-empty">
        <p>Nothing yet. Send a message and this panel fills with each step, in order:</p>
        <ol>
          <li>the conversation goes to Claude</li>
          <li>Claude asks for a tool, or answers</li>
          <li>the Go code runs the tool and reports back</li>
          <li>repeat until Claude answers in words</li>
        </ol>
      </div>
    );
  }
  const items = turn.trace.map(describe);
  const rounds = turn.trace.filter((e) => e.type === "model_start").length;
  return (
    <>
      <ol className="trace">
        {items.map((d, i) =>
          d ? (
            <li key={i} className={`trace-item trace-${d.tone}`}>
              <div className="trace-line">
                <span className="trace-title">{d.title}</span>
                {d.meta && <span className="trace-meta">{d.meta}</span>}
              </div>
              {eli5 && <p className="trace-eli5">{d.eli5}</p>}
              {d.body && (
                <details>
                  <summary>show the data</summary>
                  <pre>{d.body}</pre>
                </details>
              )}
            </li>
          ) : null,
        )}
        {turn.pending && <li className="trace-item trace-wait">waiting…</li>}
      </ol>
      <dl className="meters" title={simulated ? "Demo mode: token counts and cost are simulated by the stand-in model." : undefined}>
        <div>
          <dt>Rounds</dt>
          <dd>{rounds}</dd>
        </div>
        <div>
          <dt title="Input tokens">In</dt>
          <dd>{(turn.usage?.input_tokens ?? 0).toLocaleString("en-US")}</dd>
        </div>
        <div>
          <dt title="Output tokens">Out</dt>
          <dd>{(turn.usage?.output_tokens ?? 0).toLocaleString("en-US")}</dd>
        </div>
        <div>
          <dt title="Estimated from list prices">{simulated ? "Cost (sim.)" : "Cost"}</dt>
          <dd>${(turn.usage?.cost_usd ?? 0).toFixed(4)}</dd>
        </div>
        <div>
          <dt>Time</dt>
          <dd>{turn.latencyMs !== undefined ? `${(turn.latencyMs / 1000).toFixed(1)}s` : "…"}</dd>
        </div>
      </dl>
    </>
  );
}
