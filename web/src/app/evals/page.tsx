import type { Metadata } from "next";
import { Terminal } from "@/components/Code";
import report from "@/generated/evals.json";
import { findings, LAYER, liveRuns, liveUse, stillImperfect } from "@/content/fieldnotes";

export const metadata: Metadata = {
  title: "Evals",
  description: "The checks behind the claims: hostile inputs against each tool, a scripted model driving the real loop, and a live-model suite.",
};

type Suite = (typeof report.suites)[number];

const KIND: Record<string, { label: string; what: string }> = {
  tool: { label: "No model involved", what: "A pass holds for every model and every prompt." },
  scripted: { label: "Scripted model, real loop", what: "A fake model replays a fixed script over the real API wire format." },
  live: { label: "Real model", what: "Outcomes are checked after the real model does the task." },
};

const BASELINE = [
  { test: "TestBaseline_ReadsOutsideTheWorkingDirectory", shows: "The tutorial's read_file reads a file outside the project folder.", fixedBy: "read-parent-traversal" },
  { test: "TestBaseline_EmptyOldStrCorruptsAnExistingFile", shows: "An empty old_str turns “abc” into “XaXbXcX”.", fixedBy: "edit-empty-old-str-on-existing-file" },
  { test: "TestBaseline_ReplacesEveryMatch", shows: "A non-unique old_str rewrites every match, not one.", fixedBy: "edit-ambiguous-match" },
  { test: "TestBaseline_ListFilesPanicsOnMalformedInput", shows: "Malformed input makes list_files panic.", fixedBy: "input-wrong-type" },
];

/** A live result keeps its own date, model and commit: it is run on demand and outlives the commit it ran at. */
type Provenance = { ran_at?: string; model?: string; git_sha?: string };

const stamp = (iso: string) => new Date(iso).toISOString().slice(0, 16).replace("T", " ");

const slug = (name: string) => name.toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-|-$/g, "");

function SuiteSection({ suite }: { suite: Suite }) {
  const kind = KIND[suite.kind] ?? { label: suite.kind, what: "" };
  const categories = [...new Set(suite.cases.map((c) => c.category))];
  const from = suite as Provenance;
  return (
    <section className="suite" id={slug(suite.name)}>
      <header className="suite-head">
        <div>
          <p className="eyebrow">{kind.label}</p>
          <h2>{suite.name}</h2>
          <p>{suite.description}</p>
        </div>
        <div className={`score${suite.ran ? (suite.failed > 0 ? " score-bad" : " score-good") : " score-none"}`}>
          {suite.ran ? (
            <>
              <span className="score-num">
                {suite.passed}/{suite.total}
              </span>
              <span className="score-label">passed</span>
            </>
          ) : (
            <>
              <span className="score-num">{suite.total}</span>
              <span className="score-label">cases, not run in this report</span>
            </>
          )}
        </div>
      </header>

      {suite.ran && (
        <div className="scorebar" role="img" aria-label={`${suite.passed} of ${suite.total} cases passed`}>
          {suite.cases.map((c) => (
            <span key={c.id} className={c.passed ? "is-pass" : "is-fail"} title={`${c.id}: ${c.passed ? "pass" : "fail"}`} />
          ))}
        </div>
      )}
      {suite.ran && suite.kind === "live" && from.ran_at && (
        <p className="suite-from">
          Last run {stamp(from.ran_at)} UTC on <code>{from.model}</code>
          {from.git_sha ? (
            <>
              {" "}
              at commit <code>{from.git_sha}</code>
            </>
          ) : null}
          . This suite calls the real model and costs a few cents, so it is run on demand, not on every commit: the result shown is the latest run, and it is dropped from this page as soon as any case is changed. One run is one sample. <a href="#live-use">What it took to get here</a> is below.
        </p>
      )}
      {!suite.ran && (
        <div className="suite-note">
          <p>
            This suite calls the real model, so it needs an API key and costs a few cents. It is run on demand rather than on every commit, and this report was generated without it. The cases below are what it checks. To run it:
          </p>
          <Terminal lines={["export ANTHROPIC_API_KEY=...", "go run ./cmd/evals -live"]} />
        </div>
      )}

      {categories.map((category) => (
        <div key={category} className="table-scroll">
          <table className="table evals-table">
            <caption>{category.replace(/-/g, " ")}</caption>
            <thead>
              <tr>
                <th scope="col">Result</th>
                <th scope="col">Case</th>
                <th scope="col">The failure it exists to catch</th>
              </tr>
            </thead>
            <tbody>
              {suite.cases
                .filter((c) => c.category === category)
                .map((c) => (
                  <tr key={c.id}>
                    <td>
                      {!c.ran ? <span className="verdict verdict-none">not run</span> : c.passed ? <span className="verdict verdict-pass">pass</span> : <span className="verdict verdict-fail">fail</span>}
                    </td>
                    <td>
                      <code>{c.id}</code>
                      {"prompt" in c && c.prompt && <span className="case-prompt">“{c.prompt}”</span>}
                    </td>
                    <td>
                      {c.why}
                      {"failures" in c && Array.isArray(c.failures) && c.failures.length > 0 && (
                        <ul className="case-failures">
                          {(c.failures as string[]).map((f) => (
                            <li key={f}>{f}</li>
                          ))}
                        </ul>
                      )}
                    </td>
                  </tr>
                ))}
            </tbody>
          </table>
        </div>
      ))}
    </section>
  );
}

function LiveUse() {
  const byLayer = (["code", "prompt", "page"] as const).map((layer) => ({ layer, items: findings.filter((f) => f.layer === layer) }));
  return (
    <section className="suite" id="live-use">
      <header className="suite-head">
        <div>
          <p className="eyebrow">After the suites passed</p>
          <h2>What live use found</h2>
          <p>
            Every case above passed before launch. Then the deployed agent was used by hand: {liveUse.messages} messages in {liveUse.sessions} sessions over {liveUse.minutes} minutes on {liveUse.date}, {liveUse.searches} of them with a web search, {liveUse.costUSD.toFixed(2)} dollars in all. Each reply that was wrong was written down, traced to its cause in the stored trace, and fixed. Where a rule can check the fix, it became a test or a live case, so it cannot come back unnoticed. No suite had caught any of them.
          </p>
        </div>
        <div className="score score-good">
          <span className="score-num">{findings.length}</span>
          <span className="score-label">findings, each with its fix</span>
        </div>
      </header>

      <ul className="layer-key">
        {byLayer.map(({ layer, items }) => (
          <li key={layer}>
            <span className={`tag tag-${layer}`}>
              {items.length} {LAYER[layer].label}
            </span>
            <span>{LAYER[layer].what}</span>
          </li>
        ))}
      </ul>

      <ol className="notes">
        {findings.map((f, i) => (
          <li key={f.id} className="note" id={`finding-${f.id}`}>
            <header className="note-head">
              <span className="note-n">{String(i + 1).padStart(2, "0")}</span>
              <h3>{f.title}</h3>
              <span className={`tag tag-${f.layer}`}>{LAYER[f.layer].label}</span>
            </header>
            <dl className="note-body">
              <div>
                <dt>Asked</dt>
                <dd className="note-quote">{f.asked}</dd>
              </div>
              <div>
                <dt>Came back</dt>
                <dd>{f.got}</dd>
              </div>
              <div>
                <dt>Cause</dt>
                <dd>{f.cause}</dd>
              </div>
              <div>
                <dt>Fix</dt>
                <dd>{f.fix}</dd>
              </div>
              <div>
                <dt>Checked by</dt>
                <dd>
                  {f.checkedBy.length > 0 ? (
                    <ul className="note-checks">
                      {f.checkedBy.map((c) => (
                        <li key={c}>
                          <code>{c}</code>
                        </li>
                      ))}
                    </ul>
                  ) : (
                    !f.caveat && <span className="note-none">Seen fixed on the live site. No automated check.</span>
                  )}
                  {f.caveat && <p className="note-caveat">{f.caveat}</p>}
                </dd>
              </div>
            </dl>
          </li>
        ))}
      </ol>

      <div className="note-sub">
        <h3>The live suite was wrong twice before the agent was</h3>
        <p>
          The first two runs of the live suite each scored 14 of 15. Both times the agent had done the task and the check had named one way of doing it. The agent's code was the same in all three runs; only the checks changed. A live check has to test the outcome, not one route to it.
        </p>
        <div className="table-scroll">
          <table className="table evals-table">
            <thead>
              <tr>
                <th scope="col">Run</th>
                <th scope="col">Score</th>
                <th scope="col">What the failure turned out to be</th>
              </tr>
            </thead>
            <tbody>
              {liveRuns.map((r) => (
                <tr key={r.n}>
                  <td>{r.n}</td>
                  <td>
                    <span className={`verdict ${r.failed ? "verdict-fail" : "verdict-pass"}`}>{r.score}</span>
                    {r.failed && (
                      <span className="case-prompt">
                        <code>{r.failed}</code>
                      </span>
                    )}
                  </td>
                  <td>{r.verdict}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </div>

      <div className="note-sub">
        <h3>Still imperfect</h3>
        <ul className="plain-list">
          {stillImperfect.map((s) => (
            <li key={s}>{s}</li>
          ))}
        </ul>
      </div>
    </section>
  );
}

export default function EvalsPage() {
  const generated = new Date(report.generated_at);
  return (
    <div className="wrap">
      <div className="page-head">
        <p className="eyebrow">Evidence</p>
        <h1>Evals</h1>
        <p>
          Every safety claim on this site has a test with its name on it. There are three layers, from cheapest and most certain to most realistic. The first two run on every commit and fail the build if any case fails. Below them is the part no suite can produce: what real use of the deployed agent found.
        </p>
        <p className="page-meta">
          Report generated {generated.toISOString().slice(0, 16).replace("T", " ")} UTC
          {"git_sha" in report && report.git_sha ? ` at commit ${report.git_sha}` : ""} by <code>go run ./cmd/evals</code>. The three suites on this page are built from that file; nothing in them is typed by hand.
        </p>
      </div>

      <div className="layers">
        {report.suites.map((s, i) => (
          <a key={s.name} href={`#${slug(s.name)}`} className="layer">
            <span className="layer-n">{i + 1}</span>
            <span className="layer-name">{s.name}</span>
            <span className="layer-what">{KIND[s.kind]?.what}</span>
            <span className="layer-score">{s.ran ? `${s.passed}/${s.total} passed` : `${s.total} cases, run on demand`}</span>
          </a>
        ))}
        <a href="#live-use" className="layer">
          <span className="layer-n">{report.suites.length + 1}</span>
          <span className="layer-name">Live use</span>
          <span className="layer-what">What real conversations found after every suite had passed.</span>
          <span className="layer-score">{findings.length} findings, each with its fix</span>
        </a>
      </div>

      {report.suites.map((s) => (
        <SuiteSection key={s.name} suite={s} />
      ))}

      <LiveUse />

      <section className="suite" id="baseline">
        <header className="suite-head">
          <div>
            <p className="eyebrow">Why these cases exist</p>
            <h2>What the tutorial code does</h2>
            <p>
              These four tests run against the tutorial's finished <code>main.go</code>, unchanged, and they pass. Each one pins down a behaviour that is fine in a tutorial and unsafe on the internet, and names the eval that asserts the opposite of the deployed agent.
            </p>
          </div>
        </header>
        <div className="table-scroll">
          <table className="table evals-table">
            <thead>
              <tr>
                <th scope="col">Test in guide/steps/06-edit-file</th>
                <th scope="col">What it demonstrates</th>
                <th scope="col">Eval that shows it fixed</th>
              </tr>
            </thead>
            <tbody>
              {BASELINE.map((b) => (
                <tr key={b.test}>
                  <td>
                    <code>{b.test}</code>
                  </td>
                  <td>{b.shows}</td>
                  <td>
                    <code>{b.fixedBy}</code>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </section>

      <section className="suite">
        <h2>What these evals do not show</h2>
        <ul className="plain-list">
          <li>The first two layers say nothing about how good the model's answers are. They show the loop and the sandbox behave, whatever the model does.</li>
          <li>The live suite is small and checks outcomes with simple rules: a file contains this, the reply does not contain that. It catches regressions; it is not a benchmark.</li>
          <li>Live results vary between runs and models. A single pass is one sample, so the report records the model and the date.</li>
          <li>The prompt-injection case covers one planted instruction in one file. It shows the plumbing treats file contents as data; it does not show the model can never be persuaded.</li>
        </ul>
      </section>
    </div>
  );
}
