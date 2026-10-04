"use client";

import { useEffect, useState } from "react";

// A real run from the tutorial, replayed one line at a time: the riddle in
// secret-file.txt. Every line is visible from the start; the highlight just
// walks down them, with a plain-words caption for the line it is on.

const LINES = [
  {
    who: "You",
    tone: "you",
    text: "What's in secret-file.txt?",
    eli5: "You ask a question. It goes on the pile of notes and the whole pile is sent to Claude.",
  },
  {
    who: "tool",
    tone: "tool",
    text: 'read_file({"path":"secret-file.txt"})',
    eli5: "Claude can't open files. So it doesn't answer yet. It asks your program to run a tool.",
  },
  {
    who: "result",
    tone: "result",
    text: "what animal is the most disagreeable because it always says neigh?",
    eli5: "Your Go code opens the file and hands the text back. Claude gets another turn, without waiting for you.",
  },
  {
    who: "Claude",
    tone: "claude",
    text: "It's a riddle. The answer is a horse.",
    eli5: "Now Claude has what it needs and answers in words. No tool was asked for, so the loop stops.",
  },
] as const;

export function HeroReplay() {
  const [active, setActive] = useState(0);
  const [paused, setPaused] = useState(false);

  useEffect(() => {
    if (paused || window.matchMedia("(prefers-reduced-motion: reduce)").matches) return;
    const timer = window.setInterval(() => setActive((a) => (a + 1) % LINES.length), 2800);
    return () => window.clearInterval(timer);
  }, [paused]);

  return (
    <div className="replay" onMouseEnter={() => setPaused(true)} onMouseLeave={() => setPaused(false)} onFocus={() => setPaused(true)} onBlur={() => setPaused(false)}>
      <div className="replay-head">
        <span className="replay-dots" aria-hidden="true">
          <i /> <i /> <i />
        </span>
        <span>go run main.go</span>
      </div>
      <ol className="replay-lines">
        {LINES.map((line, i) => (
          <li key={line.who}>
            <button type="button" className={`replay-line replay-${line.tone}${i === active ? " is-active" : ""}`} aria-pressed={i === active} onClick={() => setActive(i)}>
              <span className="replay-who">{line.who}</span>
              <span className="replay-text">{line.text}</span>
            </button>
          </li>
        ))}
      </ol>
      <p className="replay-caption" aria-live="off">
        <span className="eli5-tag">#eli5</span>
        <span className="replay-step">
          {active + 1}/{LINES.length}
        </span>
        {LINES[active]!.eli5}
      </p>
    </div>
  );
}
