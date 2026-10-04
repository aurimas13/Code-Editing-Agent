"use client";

import { useEffect, useState } from "react";
import { APIError, listResearch, type ResearchAnswer } from "@/lib/api";
import { Markdown } from "./Markdown";

export function ResearchLibrary() {
  const [answers, setAnswers] = useState<ResearchAnswer[] | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    listResearch()
      .then((r) => setAnswers(r.answers))
      .catch((e) => setError(e instanceof APIError ? e.message : "Could not load the library."));
  }, []);

  if (error) {
    return (
      <div className="library-empty">
        <p className="library-empty-title">The library isn't available right now</p>
        <p>{error}</p>
      </div>
    );
  }
  if (answers === null) return <p className="thinking">Loading published answers…</p>;
  if (answers.length === 0) {
    return (
      <div className="library-empty">
        <p className="library-empty-title">No answers have been published yet</p>
        <p>
          Research answers are saved privately. One appears here only after a person has read it, checked its sources and marked it as public. Ask a question in the playground to see how an answer is produced.
        </p>
      </div>
    );
  }
  return (
    <ol className="library">
      {answers.map((a) => (
        <li key={a.id} className="answer">
          <h3>{a.question}</h3>
          <Markdown text={a.answer} />
          {a.sources.length > 0 && (
            <div className="sources">
              <span className="sources-title">Sources</span>
              <ol>
                {a.sources
                  .filter((s) => /^https?:\/\//i.test(s.url))
                  .filter((s) => s.cited || a.sources.every((x) => !x.cited))
                  .map((s) => (
                    <li key={s.url}>
                      <a href={s.url} target="_blank" rel="noopener noreferrer nofollow">
                        {s.title || s.url}
                      </a>
                    </li>
                  ))}
              </ol>
            </div>
          )}
          <p className="answer-meta">
            {new Date(a.created_at).toISOString().slice(0, 10)} · {a.model}
          </p>
        </li>
      ))}
    </ol>
  );
}
