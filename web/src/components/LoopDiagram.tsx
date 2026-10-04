// The one picture of the whole system. The same drawing is used on every
// step of the guide; `stage` says how much of it has been built so far, and
// the rest is shown faintly so the reader can see where they are heading.
//
// Colours carry meaning and match the tutorial's own terminal output:
// blue is you, yellow is Claude, green is a tool.

type Props = {
  /** 0 nothing built … 6 the finished tutorial agent, 7 the deployed version. */
  stage: number;
  /** Adds moving dots along the arrows (home page only). */
  animated?: boolean;
};

export function LoopDiagram({ stage, animated = false }: Props) {
  const on = (from: number) => (stage >= from ? "ld-on" : "ld-off");
  const toolState = (from: number) => (stage >= from ? "ld-on" : "ld-off");
  // In stage 3 read_file is defined but nothing runs it yet.
  const wired = stage >= 4 ? "ld-on" : stage === 3 ? "ld-pending" : "ld-off";

  const toClaude = stage >= 2 ? ["conversation", "+ tool menu"] : ["the whole", "conversation"];
  const fromClaude = stage >= 2 ? ["words, or", "“run a tool”"] : ["words", ""];

  const description =
    stage >= 7
      ? "You send a message through input checks to the Go program. It sends the conversation and a menu of tools to Claude. Claude replies with words or asks for a tool. The program runs read_file, list_files or edit_file inside a sandboxed workspace and sends the result back, repeating until Claude answers in words."
      : "You send a message to the Go program. It sends the conversation to Claude and prints the reply. Parts not built yet at this step are shown faintly.";

  return (
    <div className="ld-scroll">
      <svg className={`ld${animated ? " ld-animated" : ""}`} viewBox="0 0 600 332" role="img" aria-label={description}>
        <defs>
          <marker id="ld-arrow" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="7" markerHeight="7" orient="auto-start-reverse">
            <path d="M0 0 L10 5 L0 10 z" style={{ fill: "var(--ink-2)" }} />
          </marker>
        </defs>

        {/* You */}
        <g className={on(1)}>
          <rect x="8" y="40" width="100" height="72" rx="14" className="ld-you" />
          <text x="58" y="72" className="ld-title">You</text>
          <text x="58" y="92" className="ld-sub">type a message</text>
        </g>

        {/* The program */}
        <g className={on(1)}>
          <rect x="196" y="24" width="208" height="104" rx="16" className="ld-program" />
          <text x="300" y="58" className="ld-title ld-mono">main.go</text>
          <text x="300" y="81" className="ld-sub">your Go program</text>
          <text x="300" y="100" className="ld-sub">keeps the pile of notes</text>
        </g>

        {/* Claude */}
        <g className={on(1)}>
          <rect x="492" y="40" width="100" height="72" rx="14" className="ld-claude" />
          <text x="542" y="72" className="ld-title">Claude</text>
          <text x="542" y="92" className="ld-sub">reads, replies</text>
        </g>

        {/* You <-> program */}
        <g className={on(1)}>
          <line x1="110" y1="62" x2="192" y2="62" className="ld-line" markerEnd="url(#ld-arrow)" />
          <text x="151" y="40" className="ld-label">your</text>
          <text x="151" y="54" className="ld-label">message</text>
          <line x1="194" y1="92" x2="112" y2="92" className="ld-line" markerEnd="url(#ld-arrow)" />
          <text x="151" y="110" className="ld-label">the answer</text>
        </g>

        {/* program <-> Claude */}
        <g className={on(1)}>
          <line x1="406" y1="62" x2="488" y2="62" className="ld-line" markerEnd="url(#ld-arrow)" />
          <text x="447" y="40" className="ld-label">{toClaude[0]}</text>
          <text x="447" y="54" className="ld-label">{toClaude[1]}</text>
          <line x1="490" y1="92" x2="408" y2="92" className="ld-line" markerEnd="url(#ld-arrow)" />
          <text x="447" y="110" className="ld-label">{fromClaude[0]}</text>
          <text x="447" y="124" className="ld-label">{fromClaude[1]}</text>
          {animated && (
            <>
              <circle r="4.5" cx="-20" cy="-20" className="ld-dot ld-dot-out"><animateMotion dur="2.4s" repeatCount="indefinite" path="M426 82 L508 82" /></circle>
              <circle r="4.5" cx="-20" cy="-20" className="ld-dot ld-dot-back"><animateMotion dur="2.4s" repeatCount="indefinite" path="M510 112 L428 112" /></circle>
            </>
          )}
        </g>

        {/* checks on the way in (deployed version only) */}
        <g className={on(7)}>
          <line x1="151" y1="116" x2="151" y2="140" className="ld-line ld-thin" />
          <rect x="52" y="140" width="198" height="26" rx="13" className="ld-check" />
          <text x="151" y="157" className="ld-chip">checks: size, rate, daily budget</text>
        </g>

        {/* program -> tools */}
        <g className={wired}>
          <path d="M300 130 L300 178 M130 178 L470 178 M130 178 L130 208 M300 178 L300 208 M470 178 L470 208" className="ld-line ld-bus" />
          <text x="312" y="156" className="ld-label ld-start">runs the tool, sends back the result</text>
          {animated && (
            <circle r="4.5" cx="-20" cy="-20" className="ld-dot ld-dot-tool"><animateMotion dur="2.4s" repeatCount="indefinite" path="M320 150 L320 228 L320 150" /></circle>
          )}
        </g>

        {/* the sandbox (deployed version only) */}
        <g className={on(7)}>
          <rect x="36" y="194" width="528" height="112" rx="18" className="ld-sandbox" />
          <rect x="52" y="296" width="252" height="20" className="ld-sandbox-tag" />
          <text x="62" y="311" className="ld-chip ld-start">sandbox: the only files it can touch</text>
        </g>

        {/* tools */}
        <g className={stage === 3 ? "ld-pending" : toolState(3)}>
          <rect x="55" y="210" width="150" height="44" rx="22" className="ld-tool" />
          <text x="130" y="237" className="ld-title ld-mono ld-small">read_file</text>
          <text x="130" y="276" className="ld-sub">open a file</text>
        </g>
        <g className={toolState(5)}>
          <rect x="225" y="210" width="150" height="44" rx="22" className="ld-tool" />
          <text x="300" y="237" className="ld-title ld-mono ld-small">list_files</text>
          <text x="300" y="276" className="ld-sub">see what is there</text>
        </g>
        <g className={toolState(6)}>
          <rect x="395" y="210" width="150" height="44" rx="22" className="ld-tool" />
          <text x="470" y="237" className="ld-title ld-mono ld-small">edit_file</text>
          <text x="470" y="276" className="ld-sub">change some text</text>
        </g>
      </svg>
    </div>
  );
}
