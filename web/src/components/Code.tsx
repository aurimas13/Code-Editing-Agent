import type { ReactNode } from "react";
import { highlight, type Lang, type Token } from "@/lib/highlight";

export function Tokens({ tokens }: { tokens: Token[] | undefined }) {
  if (!tokens || tokens.length === 0) return null; // CSS keeps the empty row one line tall
  return (
    <>
      {tokens.map((t, i) =>
        t.kind === "plain" ? t.text : (
          <span key={i} className={`tok-${t.kind}`}>
            {t.text}
          </span>
        ),
      )}
    </>
  );
}

export type CodeRowProps = {
  no?: number | string;
  kind?: "same" | "add" | "del";
  tokens?: Token[];
  id?: string;
};

/** One line of code with its gutter. */
export function CodeRow({ no, kind = "same", tokens, id }: CodeRowProps) {
  return (
    <div className={`code-row code-${kind}`} id={id}>
      <span className="code-no" aria-hidden="true">
        {no ?? ""}
      </span>
      <span className="code-mark" aria-hidden="true">
        {kind === "add" ? "+" : kind === "del" ? "−" : ""}
      </span>
      <code className="code-text">
        <Tokens tokens={tokens} />
      </code>
    </div>
  );
}

/** A static, highlighted block of code. */
export function CodeBlock({
  code,
  lang = "text",
  title,
  numbers = false,
  children,
}: {
  code: string;
  lang?: Lang;
  title?: string;
  numbers?: boolean;
  children?: ReactNode;
}) {
  const lines = highlight(code.replace(/\n$/, ""), lang);
  return (
    <figure className="codeblock">
      {(title || children) && (
        <figcaption className="codeblock-head">
          <span className="codeblock-title">{title}</span>
          {children}
        </figcaption>
      )}
      <div className="code-scroll" tabIndex={0}>
        <div className={`code-lines${numbers ? "" : " code-plain"}`}>
          {lines.map((tokens, i) => (
            <CodeRow key={i} no={numbers ? i + 1 : undefined} tokens={tokens} />
          ))}
        </div>
      </div>
    </figure>
  );
}

/** Shell commands, one per line, each with a prompt. */
export function Terminal({ lines, label = "Terminal" }: { lines: string[]; label?: string }) {
  return (
    <figure className="terminal">
      <figcaption className="terminal-head">{label}</figcaption>
      <div className="code-scroll" tabIndex={0}>
        <pre className="terminal-body">
          {lines.map((line, i) => (
            <span key={i} className="terminal-line">
              <span className="terminal-prompt" aria-hidden="true">
                ${" "}
              </span>
              {line}
              {"\n"}
            </span>
          ))}
        </pre>
      </div>
    </figure>
  );
}
