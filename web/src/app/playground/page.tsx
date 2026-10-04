import type { Metadata } from "next";
import { Playground } from "@/components/Playground";

export const metadata: Metadata = {
  title: "Playground",
  description: "Give the agent a task, watch it choose tools, and see every step and file change as it happens.",
};

export default function PlaygroundPage() {
  return (
    <div className="wrap wrap-wide page-tight">
      <div className="page-head page-head-row">
        <h1>Playground</h1>
        <p>
          Give the agent a task. It decides which tools to use; you see every step. Its files live in a sandbox that belongs to your session and nothing else.
        </p>
      </div>
      <Playground />
    </div>
  );
}
