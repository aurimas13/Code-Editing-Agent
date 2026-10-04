"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import { CodeRow, Terminal } from "./Code";
import { LoopDiagram } from "./LoopDiagram";
import { guideSteps, productionChanges, type GuideStep } from "@/content/guide";
import { diffLines, findHunks, splitLines, type DiffLine } from "@/lib/diff";
import { buildOutline, describePlacement, parseDecls, type Placement } from "@/lib/outline";
import { highlight, type Token } from "@/lib/highlight";

type Checkpoint = { id: string; code: string };

/** One place in the file the reader has to type something. */
type Spot = Placement & {
  n: number;
  /** New-file line range. */
  start: number;
  end: number;
  added: number;
  removed: number;
  /** Index into the diff of the first changed line. */
  diffIndex: number;
};

type StepView = {
  diff: DiffLine[];
  spots: Spot[];
  newTokens: Token[][];
  oldTokens: Token[][];
  totalLines: number;
  added: number;
  removed: number;
};

function buildView(code: string, previous: string): StepView {
  const newLines = splitLines(code);
  const oldLines = splitLines(previous);
  const diff = diffLines(oldLines, newLines);
  const addedSet = new Set(diff.filter((d) => d.kind === "add").map((d) => d.newNo!));

  const existing = new Set(parseDecls(oldLines).map((d) => d.name));

  const spots: Spot[] = [];
  for (const h of findHunks(diff)) {
    // A hunk that only adds blank lines is spacing, not something to point at.
    const onlyBlank = diff.slice(h.index, h.index + h.added + h.removed).every((d) => d.text.trim() === "");
    if (onlyBlank) continue;
    const place = describePlacement(newLines, h.start, h.end, addedSet, existing);
    const last = spots[spots.length - 1];
    // Neighbouring hunks in the same place read better as one instruction.
    if (last && last.label === place.label && h.start - last.end <= 3) {
      last.end = h.end;
      last.added += h.added;
      last.removed += h.removed;
      continue;
    }
    spots.push({ ...place, n: spots.length + 1, start: h.start, end: h.end, added: h.added, removed: h.removed, diffIndex: h.index });
  }

  return {
    diff,
    spots,
    newTokens: highlight(newLines.join("\n"), "go"),
    oldTokens: highlight(oldLines.join("\n"), "go"),
    totalLines: newLines.length,
    added: diff.filter((d) => d.kind === "add").length,
    removed: diff.filter((d) => d.kind === "del").length,
  };
}

/** Inline `code` spans in the guide's prose. */
function Prose({ text }: { text: string }) {
  const parts = text.split(/(`[^`]+`)/g);
  return (
    <>
      {parts.map((p, i) => (p.startsWith("`") && p.endsWith("`") ? <code key={i}>{p.slice(1, -1)}</code> : p))}
    </>
  );
}

const CONTEXT = 3;

export function GuideExplorer({ checkpoints }: { checkpoints: Checkpoint[] }) {
  const [index, setIndex] = useState(0);
  const [wholeFile, setWholeFile] = useState(true);
  const [showRemoved, setShowRemoved] = useState(true);
  const [activeSpot, setActiveSpot] = useState<number | null>(null);
  const codeRef = useRef<HTMLDivElement>(null);
  const topRef = useRef<HTMLDivElement>(null);

  const step = guideSteps[index]!;
  const navRef = useRef<HTMLElement>(null);

  // Keep the current step's button visible in the (scrollable) step bar.
  useEffect(() => {
    const bar = navRef.current;
    const current = bar?.querySelector<HTMLElement>(".is-current");
    if (bar && current) bar.scrollTo({ left: current.offsetLeft - bar.clientWidth / 2 + current.clientWidth / 2 });
  }, [index]);

  // Deep links: /guide#04-tool-loop opens that step.
  useEffect(() => {
    const fromHash = () => {
      const i = guideSteps.findIndex((s) => s.id === window.location.hash.slice(1));
      if (i >= 0) setIndex(i);
    };
    fromHash();
    window.addEventListener("hashchange", fromHash);
    return () => window.removeEventListener("hashchange", fromHash);
  }, []);

  const view = useMemo(() => {
    if (!step.checkpoint) return null;
    const at = checkpoints.findIndex((c) => c.id === step.checkpoint);
    if (at < 0) return null;
    return buildView(checkpoints[at]!.code, at > 0 ? checkpoints[at - 1]!.code : "");
  }, [step, checkpoints]);

  const go = (i: number) => {
    setIndex(i);
    setActiveSpot(null);
    window.history.replaceState(null, "", `#${guideSteps[i]!.id}`);
    codeRef.current?.scrollTo({ top: 0 });
    topRef.current?.scrollIntoView({ block: "start", behavior: "smooth" });
  };

  const jumpTo = (spot: Spot) => {
    setActiveSpot(spot.n);
    const row = codeRef.current?.querySelector<HTMLElement>(`[data-spot="${spot.n}"]`);
    const box = codeRef.current;
    if (row && box) box.scrollTo({ top: row.offsetTop - 12, behavior: "smooth" });
  };

  const isFirstCheckpoint = step.checkpoint === checkpoints[0]?.id;

  return (
    <div className="guide" ref={topRef}>
      <nav className="guide-nav" aria-label="Steps" ref={navRef}>
        <ol>
          {guideSteps.map((s, i) => (
            <li key={s.id}>
              <button type="button" className={`guide-navbtn${i === index ? " is-current" : ""}${i < index ? " is-done" : ""}`} aria-current={i === index ? "step" : undefined} onClick={() => go(i)}>
                <span className="guide-navnum">{i}</span>
                <span className="guide-navlabel">{s.nav}</span>
              </button>
            </li>
          ))}
        </ol>
      </nav>

      <div className="guide-cols">
        <article className="guide-text">
          <p className="eyebrow">
            Step {index} of {guideSteps.length - 1}
          </p>
          <h2>{step.title}</h2>

          <div className="eli5">
            <span className="eli5-tag">#eli5</span>
            {step.eli5.map((p, i) => (
              <p key={i}>
                <Prose text={p} />
              </p>
            ))}
          </div>

          <figure className="guide-figure">
            <LoopDiagram stage={step.stage} />
            <figcaption>
              {step.stage === 0 && "Nothing is built yet. This is the finished picture, shown faintly."}
              {step.stage > 0 && step.stage < 7 && "Solid parts exist after this step. Faint parts come later."}
              {step.stage === 7 && "The same loop, with a sandbox around the tools and checks on the way in."}
            </figcaption>
          </figure>

          <h3>What you add</h3>
          <ul className="guide-adds">
            {step.adds.map((a, i) => (
              <li key={i}>
                <Prose text={a} />
              </li>
            ))}
          </ul>

          {view && (
            <>
              <h3>Where it goes in main.go</h3>
              {isFirstCheckpoint ? (
                <p className="guide-note">
                  The file is empty, so all {view.totalLines} lines are new. Type or paste them in the order shown.
                </p>
              ) : (
                <ol className="spots">
                  {view.spots.map((spot) => (
                    <li key={spot.n}>
                      <button type="button" className={`spot${activeSpot === spot.n ? " is-active" : ""}`} onClick={() => jumpTo(spot)}>
                        <span className="spot-n">{spot.n}</span>
                        <span className="spot-body">
                          <span className="spot-label">
                            <Prose text={spot.label.replace(/(func .+|type \w+|var \w+)$/, "`$1`")} />
                          </span>
                          <span className="spot-meta">
                            {spot.added > 0 && <span className="spot-add">+{spot.added} {spot.added === 1 ? "line" : "lines"}</span>}
                            {spot.removed > 0 && <span className="spot-del">−{spot.removed} replaced</span>}
                            <span>
                              line{spot.end > spot.start ? "s" : ""} {spot.start}
                              {spot.end > spot.start ? `–${spot.end}` : ""}
                            </span>
                          </span>
                        </span>
                      </button>
                    </li>
                  ))}
                </ol>
              )}
            </>
          )}

          {step.commands && (
            <>
              <h3>{step.checkpoint ? "Run it" : "Type these in your terminal"}</h3>
              <Terminal lines={step.commands} />
            </>
          )}

          {step.tryIt && (
            <dl className="tryit">
              <div>
                <dt>Say</dt>
                <dd className="tryit-say">{step.tryIt.say}</dd>
              </div>
              <div>
                <dt>You should see</dt>
                <dd>
                  <Prose text={step.tryIt.see} />
                </dd>
              </div>
            </dl>
          )}

          {step.watchOut && (
            <p className="watchout">
              <strong>Watch out.</strong> <Prose text={step.watchOut} />
            </p>
          )}

          {step.checkpoint && (
            <p className="guide-note">
              This exact file is in the repository at <code>guide/steps/{step.checkpoint}/main.go</code>. It is compiled on every commit.
            </p>
          )}

          <div className="guide-pager">
            <button type="button" className="btn btn-quiet" disabled={index === 0} onClick={() => go(index - 1)}>
              ← Back
            </button>
            {index < guideSteps.length - 1 ? (
              <button type="button" className="btn btn-primary" onClick={() => go(index + 1)}>
                Next: {guideSteps[index + 1]!.nav} →
              </button>
            ) : (
              <a className="btn btn-primary" href="/playground">
                Try the finished agent →
              </a>
            )}
          </div>
        </article>

        <section className="guide-code" aria-label="main.go at this step">
          {view ? (
            <FileView
              step={step}
              view={view}
              wholeFile={wholeFile || isFirstCheckpoint}
              showRemoved={showRemoved}
              canToggle={!isFirstCheckpoint}
              onWholeFile={setWholeFile}
              onShowRemoved={setShowRemoved}
              activeSpot={activeSpot}
              codeRef={codeRef}
              onJump={jumpTo}
            />
          ) : step.id === "07-production" ? (
            <ProductionMap />
          ) : (
            <FileOutline checkpoints={checkpoints} onStep={(checkpointIndex) => go(guideSteps.findIndex((s) => s.checkpoint === checkpoints[checkpointIndex]?.id))} />
          )}
        </section>
      </div>
    </div>
  );
}

function FileView({
  step,
  view,
  wholeFile,
  showRemoved,
  canToggle,
  onWholeFile,
  onShowRemoved,
  activeSpot,
  codeRef,
  onJump,
}: {
  step: GuideStep;
  view: StepView;
  wholeFile: boolean;
  showRemoved: boolean;
  canToggle: boolean;
  onWholeFile: (v: boolean) => void;
  onShowRemoved: (v: boolean) => void;
  activeSpot: number | null;
  codeRef: React.RefObject<HTMLDivElement | null>;
  onJump: (s: Spot) => void;
}) {
  const spotAt = new Map(view.spots.map((s) => [s.diffIndex, s]));

  // In "changes only" mode keep a few lines of context around each change.
  const visible = new Set<number>();
  if (!wholeFile) {
    view.diff.forEach((d, i) => {
      if (d.kind === "same") return;
      for (let k = i - CONTEXT; k <= i + CONTEXT; k++) if (k >= 0 && k < view.diff.length) visible.add(k);
    });
  }

  const rows: React.ReactNode[] = [];
  let hidden = 0;
  const flushHidden = (key: string) => {
    if (hidden > 0) {
      rows.push(
        <div className="code-gap" key={key}>
          {hidden} unchanged {hidden === 1 ? "line" : "lines"}
        </div>,
      );
      hidden = 0;
    }
  };
  view.diff.forEach((d, i) => {
    if (d.kind === "del" && !showRemoved) return;
    if (!wholeFile && !visible.has(i)) {
      hidden++;
      return;
    }
    flushHidden(`gap-${i}`);
    const spot = spotAt.get(i);
    if (spot && canToggle) {
      rows.push(
        <div className={`code-banner${activeSpot === spot.n ? " is-active" : ""}`} key={`banner-${i}`} data-spot={spot.n}>
          <span className="spot-n">{spot.n}</span>
          {spot.label}
        </div>,
      );
    }
    rows.push(
      <CodeRow
        key={i}
        kind={canToggle ? d.kind : "same"}
        no={d.kind === "del" ? "" : d.newNo}
        tokens={d.kind === "del" ? view.oldTokens[d.oldNo! - 1] : view.newTokens[d.newNo! - 1]}
      />,
    );
  });
  flushHidden("gap-end");

  return (
    <div className="filecard">
      <div className="filecard-head">
        <span className="filecard-name">main.go</span>
        <span className="filecard-meta">
          after step “{step.nav}” · {view.totalLines} lines
          {canToggle && (
            <>
              {" · "}
              <span className="spot-add">+{view.added}</span>
              {view.removed > 0 && (
                <>
                  {" "}
                  <span className="spot-del">−{view.removed}</span>
                </>
              )}
            </>
          )}
        </span>
        {canToggle && (
          <div className="filecard-tools">
            <div className="seg" role="group" aria-label="How much of the file to show">
              <button type="button" className={wholeFile ? "is-on" : ""} aria-pressed={wholeFile} onClick={() => onWholeFile(true)}>
                Whole file
              </button>
              <button type="button" className={!wholeFile ? "is-on" : ""} aria-pressed={!wholeFile} onClick={() => onWholeFile(false)}>
                Changes only
              </button>
            </div>
            {view.removed > 0 && (
              <label className="check">
                <input id="guide-show-removed" type="checkbox" checked={showRemoved} onChange={(e) => onShowRemoved(e.target.checked)} />
                Show replaced lines
              </label>
            )}
          </div>
        )}
      </div>

      <div className="filecard-body">
        {canToggle && (
          <div className="minimap" aria-hidden="true">
            {view.spots.map((s) => (
              <button
                type="button"
                key={s.n}
                tabIndex={-1}
                className={`minimap-mark${activeSpot === s.n ? " is-active" : ""}`}
                style={{
                  top: `${((s.start - 1) / view.totalLines) * 100}%`,
                  height: `max(6px, ${((s.end - s.start + 1) / view.totalLines) * 100}%)`,
                }}
                onClick={() => onJump(s)}
              />
            ))}
          </div>
        )}
        <div className="code-scroll guide-codescroll" ref={codeRef} tabIndex={0}>
          <div className="code-lines">{rows}</div>
        </div>
      </div>
      {canToggle && (
        <p className="filecard-foot">
          <span className="legend legend-add" /> add this line
          <span className="legend legend-del" /> the line it replaces
          <span className="legend-bar" /> the strip on the left is the whole file; green marks are where you type
        </p>
      )}
    </div>
  );
}

/** The finished file as a list of its parts, each tagged with the step that adds it. */
function FileOutline({ checkpoints, onStep }: { checkpoints: Checkpoint[]; onStep: (checkpointIndex: number) => void }) {
  const rows = useMemo(() => buildOutline(checkpoints.map((c) => c.code)), [checkpoints]);
  const total = rows[rows.length - 1]?.end ?? 0;
  const stepNumber = (checkpointIndex: number) => guideSteps.findIndex((s) => s.checkpoint === checkpoints[checkpointIndex]?.id);
  return (
    <div className="filecard">
      <div className="filecard-head">
        <span className="filecard-name">main.go</span>
        <span className="filecard-meta">the finished file, {total} lines · which step adds each part</span>
      </div>
      <ol className="outline">
        {rows.map((row) => (
          <li key={row.name} className="outline-row">
            <span className="outline-lines">
              {row.start}–{row.end}
            </span>
            <code className="outline-name">{row.name}</code>
            <span className="outline-steps">
              <button type="button" className="chip chip-add" onClick={() => onStep(row.introduced)}>
                added in step {stepNumber(row.introduced)}
              </button>
              {row.changed.map((c) => (
                <button type="button" key={c} className="chip chip-edit" onClick={() => onStep(c)}>
                  edited in {stepNumber(c)}
                </button>
              ))}
            </span>
          </li>
        ))}
      </ol>
      <p className="filecard-foot">Everything goes in one file, top to bottom in this order. Each step shows the exact lines.</p>
    </div>
  );
}

const PACKAGE_MAP: { from: string; to: string; why: string }[] = [
  { from: "Run, executeTool, runInference", to: "internal/agent", why: "The loop, now with a round limit, approval hook and an event for every step." },
  { from: "ToolDefinition, GenerateSchema", to: "internal/tools/tools.go", why: "The tool menu. Schemas now mark required fields." },
  { from: "ReadFile, ListFiles, EditFile", to: "internal/tools/files.go", why: "Same three tools, with the edit rules fixed." },
  { from: "os.ReadFile, os.WriteFile, filepath.Walk", to: "internal/workspace", why: "All file access, confined to a sandbox." },
  { from: "client.Messages.New", to: "internal/llm", why: "The model call, streaming, behind an interface so it can be faked in tests." },
  { from: "main()", to: "cmd/agent, cmd/server", why: "Two front doors: the terminal chat and the HTTP API this site uses." },
  { from: "(new)", to: "internal/guardrails", why: "Input checks, redaction, rate limits, the daily budget." },
  { from: "(new)", to: "internal/store", why: "Sessions and traces in Supabase, or in memory for local runs." },
  { from: "(new)", to: "internal/evals, evals/", why: "The checks that prove the rows below." },
];

function ProductionMap() {
  return (
    <div className="filecard">
      <div className="filecard-head">
        <span className="filecard-name">main.go → packages</span>
        <span className="filecard-meta">where each piece of the tutorial file went</span>
      </div>
      <div className="prodmap">
        <table className="table">
          <thead>
            <tr>
              <th scope="col">In main.go</th>
              <th scope="col">Now lives in</th>
              <th scope="col">What changed</th>
            </tr>
          </thead>
          <tbody>
            {PACKAGE_MAP.map((row) => (
              <tr key={row.to}>
                <td>
                  <code>{row.from}</code>
                </td>
                <td>
                  <code className="code-strong">{row.to}</code>
                </td>
                <td>{row.why}</td>
              </tr>
            ))}
          </tbody>
        </table>

        <h3>What the tutorial version does, and what this one does instead</h3>
        <div className="changes">
          {productionChanges.map((c) => (
            <div className="change" key={c.area}>
              <h4>{c.area}</h4>
              <p className="change-before">
                <span className="change-tag">Tutorial</span>
                <Prose text={c.tutorial} />
              </p>
              <p className="change-after">
                <span className="change-tag">Now</span>
                <Prose text={c.now} />
              </p>
              <p className="change-meta">
                <code>{c.where}</code> · checked by {c.evidence}
              </p>
            </div>
          ))}
        </div>
      </div>
    </div>
  );
}
