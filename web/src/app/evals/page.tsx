import type { Metadata } from "next";
import { Terminal } from "@/components/Code";
import report from "@/generated/evals.json";

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

const slug = (name: string) => name.toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-|-$/g, "");

function SuiteSection({ suite }: { suite: Suite }) {
  const kind = KIND[suite.kind] ?? { label: suite.kind, what: "" };
  const categories = [...new Set(suite.cases.map((c) => c.category))];
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

export default function EvalsPage() {
  const generated = new Date(report.generated_at);
  return (
    <div className="wrap">
      <div className="page-head">
        <p className="eyebrow">Evidence</p>
        <h1>Evals</h1>
        <p>
          Every safety claim on this site has a test with its name on it. There are three layers, from cheapest and most certain to most realistic. The first two run on every commit and fail the build if any case fails.
        </p>
        <p className="page-meta">
          Report generated {generated.toISOString().slice(0, 16).replace("T", " ")} UTC
          {"git_sha" in report && report.git_sha ? ` at commit ${report.git_sha}` : ""} by <code>go run ./cmd/evals</code>. This page is built from that file; nothing here is typed by hand.
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
      </div>

      {report.suites.map((s) => (
        <SuiteSection key={s.name} suite={s} />
      ))}

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
