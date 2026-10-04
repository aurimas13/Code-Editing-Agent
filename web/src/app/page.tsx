import Link from "next/link";
import { HeroReplay } from "@/components/HeroReplay";
import { LoopDiagram } from "@/components/LoopDiagram";
import evals from "@/generated/evals.json";
import guide from "@/generated/guide.json";
import { site } from "@/lib/site";

const ADDED = [
  {
    title: "A sandbox, not a promise",
    body: "The tutorial agent can read and write any file its user can. Here every path is checked in one place and resolved inside a workspace the agent cannot leave. That is enforced by Go code, so no prompt can talk its way around it.",
    proof: "sandbox evals",
    href: "/evals#sandbox-and-edit-rules",
  },
  {
    title: "Edits that can't silently wreck a file",
    body: "Two inputs made the tutorial's edit_file damage files without an error. Both are now refused with a message the model can act on.",
    proof: "edit-rule evals",
    href: "/evals#sandbox-and-edit-rules",
  },
  {
    title: "Limits on everything that costs money",
    body: "Rounds per message, messages per session, messages per visitor, and a daily budget in dollars that survives restarts. When the budget is gone the model is not called.",
    proof: "limits",
    href: "/architecture#limits",
  },
  {
    title: "Every step visible and stored",
    body: "Each model call, tool call, result and guardrail is an event. The browser shows them live, the database keeps them as a trace, and the tests assert on them.",
    proof: "X-ray panel",
    href: "/playground",
  },
  {
    title: "Web research with sources",
    body: "A second mode adds web search. Answers come with the pages they rely on, and are kept private until a person reviews and publishes them.",
    proof: "research mode",
    href: "/research",
  },
  {
    title: "Evals, not vibes",
    body: "Hostile inputs against each tool, a scripted model driving the real loop over the real wire format, and a live-model suite that checks the files the agent leaves behind.",
    proof: "eval report",
    href: "/evals",
  },
];

export default function Home() {
  const ran = evals.suites.filter((s) => s.ran);
  const passed = ran.reduce((n, s) => n + s.passed, 0);
  const total = ran.reduce((n, s) => n + s.total, 0);
  const finalLines = guide.steps[guide.steps.length - 1]!.code.split("\n").length - 1;

  return (
    <>
      <section className="hero">
        <div className="wrap hero-grid">
          <div className="hero-copy">
            <p className="eyebrow">A code-editing agent in Go · agent no. 3 by {site.author}</p>
            <h1>
              An LLM, a loop, and <span className="mark">three tools</span>.
            </h1>
            <p className="hero-lede">
              That is all a code-editing agent is. This one is built from a {finalLines}-line tutorial file, then made safe enough to put on the internet: sandboxed, rate-limited, traced and tested. Try it, or build it yourself with a guide that shows where every line goes.
            </p>
            <div className="hero-cta">
              <Link href="/playground" className="btn btn-primary btn-large">
                Try the agent
              </Link>
              <Link href="/guide" className="btn btn-secondary btn-large">
                Build it step by step
              </Link>
            </div>
            <dl className="hero-facts">
              <div>
                <dt>
                  {passed}/{total}
                </dt>
                <dd>deterministic evals pass on every commit</dd>
              </div>
              <div>
                <dt>{guide.steps.length}</dt>
                <dd>tutorial checkpoints, each one compiled in CI</dd>
              </div>
              <div>
                <dt>0</dt>
                <dd>lines of model-written code run on the server</dd>
              </div>
            </dl>
          </div>
          <HeroReplay />
        </div>
      </section>

      <section className="section">
        <div className="wrap split">
          <div className="split-text">
            <p className="eyebrow">The whole idea</p>
            <h2>Claude can't touch anything. Your program can.</h2>
            <div className="eli5">
              <span className="eli5-tag">#eli5</span>
              <p>
                Claude is a clever friend on the phone. It can't see your desk or pick anything up. So you agree on three favours it may ask for: “read me that page”, “tell me what's on the desk”, and “swap these words for those”.
              </p>
              <p>
                Your program listens. When Claude asks for a favour, the program does it and reads the result back. Then Claude might ask for another, or say “done, here's your answer”. That back-and-forth is the loop, and the loop is the agent.
              </p>
            </div>
            <p>
              The colours on this site mean something, and they come from the tutorial's own terminal output: <span className="swatch swatch-you">blue is you</span>, <span className="swatch swatch-claude">yellow is Claude</span>, <span className="swatch swatch-tool">green is a tool</span>.
            </p>
          </div>
          <figure className="split-figure">
            <LoopDiagram stage={7} animated />
            <figcaption>The deployed agent. The tutorial version is the same picture without the sandbox and the checks.</figcaption>
          </figure>
        </div>
      </section>

      <section className="section section-tint">
        <div className="wrap">
          <p className="eyebrow">From tutorial to production</p>
          <h2>What it takes before strangers can use it</h2>
          <p className="section-lede">
            The loop itself did not change. What changed is everything around it. Each item links to the evidence.
          </p>
          <ul className="added">
            {ADDED.map((item) => (
              <li key={item.title}>
                <h3>{item.title}</h3>
                <p>{item.body}</p>
                <Link href={item.href} className="added-proof">
                  See the {item.proof} →
                </Link>
              </li>
            ))}
          </ul>
        </div>
      </section>

      <section className="section">
        <div className="wrap doors">
          <Link href="/playground" className="door door-claude">
            <span className="door-kicker">Use it</span>
            <span className="door-title">Playground</span>
            <span className="door-body">Give it a task. Watch it read, list and edit files in your own sandbox, with an X-ray of every step.</span>
          </Link>
          <Link href="/guide" className="door door-tool">
            <span className="door-kicker">Build it</span>
            <span className="door-title">Step-by-step guide</span>
            <span className="door-body">Seven steps. The whole file after each one, the new lines marked, and where they go said in plain words.</span>
          </Link>
          <Link href="/architecture" className="door door-you">
            <span className="door-kicker">Inspect it</span>
            <span className="door-title">Architecture and evals</span>
            <span className="door-body">How a request flows, where each limit lives, what is stored, and the report that backs the claims.</span>
          </Link>
        </div>
      </section>

      <section className="section section-tint">
        <div className="wrap about">
          <div>
            <p className="eyebrow">Who made this</p>
            <h2>{site.author}</h2>
            <p>
              I build AI products and teach Python and AI to working professionals in Lithuania and Latvia. This is the third agent I have built from first principles, each one to understand a different part of the stack by making it work end to end.
            </p>
            <p>
              The agent's core follows Thorsten Ball's tutorial. The refactor, the sandbox, the limits, the evals and this site were built with Claude as a pair programmer; the design decisions and the reasons for them are written down in the repository.
            </p>
            <p className="about-links">
              <a href={site.authorUrl}>aurimas.io</a>
              <a href={site.github} target="_blank" rel="noopener noreferrer">
                GitHub
              </a>
              <a href={site.repo} target="_blank" rel="noopener noreferrer">
                This repository
              </a>
            </p>
          </div>
          <div>
            <h3>The other two agents</h3>
            <ul className="agents">
              {site.otherAgents.map((a) => (
                <li key={a.url}>
                  <a href={a.url} target="_blank" rel="noopener noreferrer">
                    {a.name}
                  </a>
                  <span>{a.note}</span>
                </li>
              ))}
              <li>
                <span className="agents-here">Code-Editing Agent</span>
                <span>Go, this site</span>
              </li>
            </ul>
          </div>
        </div>
      </section>
    </>
  );
}
