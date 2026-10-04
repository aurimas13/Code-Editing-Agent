import type { Metadata } from "next";
import { GuideExplorer } from "@/components/GuideExplorer";
import guide from "@/generated/guide.json";
import { site } from "@/lib/site";

export const metadata: Metadata = {
  title: "Build guide",
  description: "Build the agent in seven steps. Each step shows main.go as it should look, with the lines to add and where they go.",
};

export default function GuidePage() {
  return (
    <div className="wrap wrap-wide">
      <div className="page-head">
        <p className="eyebrow">Build it yourself</p>
        <h1>The agent, one step at a time</h1>
        <p>
          This follows{" "}
          <a href={site.tutorial} target="_blank" rel="noopener noreferrer">
            Thorsten Ball's tutorial
          </a>
          , which is excellent and assumes you already know where things go in a Go file. This version shows the whole of <code>main.go</code> after every step, marks the lines to add, and says in words where each one belongs. Each explanation is written so a five-year-old could follow the idea.
        </p>
      </div>
      <GuideExplorer checkpoints={guide.steps} />
    </div>
  );
}
