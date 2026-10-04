import type { Metadata } from "next";
import Link from "next/link";
import { CodeBlock } from "@/components/Code";
import { ResearchLibrary } from "@/components/ResearchLibrary";
import agent from "@/generated/agent.json";

export const metadata: Metadata = {
  title: "Research",
  description: "The same loop with one more tool: web search. Answers come with sources and are published only after a person reviews them.",
};

export default function ResearchPage() {
  const researchPart = agent.system_prompt_research.slice(agent.system_prompt_research.indexOf("Research"), agent.system_prompt_research.indexOf("Teaching")).trim();
  return (
    <div className="wrap">
      <div className="page-head">
        <p className="eyebrow">Research mode</p>
        <h1>One more tool: looking things up</h1>
        <p>
          The loop does not care what a tool does. Give it web search and the same agent that edits files can answer questions that need current facts, and write what it finds into a file.
        </p>
        <div className="hero-cta">
          <Link href="/playground?mode=research" className="btn btn-primary">
            Ask a research question
          </Link>
        </div>
      </div>

      <section className="section-plain split">
        <div className="split-text">
          <div className="eli5">
            <span className="eli5-tag">#eli5</span>
            <p>
              Your clever friend on the phone knows a lot, but only what they had learned by the day they last studied. Ask about last week and they have to guess.
            </p>
            <p>
              So you add a fourth favour to the menu: “go to the library and look this up”. Now they can check before answering, and tell you which books they used so you can check too.
            </p>
          </div>
          <h2>How an answer gets here</h2>
          <ol className="steps-list">
            <li>
              <strong>Search, capped.</strong> At most {agent.limits.web_search_max_uses} searches per model call and {agent.limits.research_per_day} research questions per visitor per day. Search runs on Anthropic's side; this server never fetches a URL the model chose.
            </li>
            <li>
              <strong>Answer with sources.</strong> The pages returned and the ones actually cited are recorded with the answer.
            </li>
            <li>
              <strong>Saved privately.</strong> The question, answer and sources go into Supabase with <code>is_public = false</code>.
            </li>
            <li>
              <strong>Published by a person.</strong> An answer appears below only after someone has read it and flipped that flag. The agent cannot publish its own work.
            </li>
          </ol>
        </div>
        <div className="split-figure">
          <CodeBlock code={researchPart} title="What the system prompt adds in research mode" />
          <p className="figure-note">This is the text the deployed agent is given, exported from the Go source at build time.</p>
        </div>
      </section>

      <section className="section-plain">
        <h2>Published answers</h2>
        <ResearchLibrary />
      </section>
    </div>
  );
}
