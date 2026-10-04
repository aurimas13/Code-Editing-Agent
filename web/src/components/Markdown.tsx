import type { ReactNode } from "react";

// A deliberately small Markdown renderer for model replies. It builds React
// elements, never HTML strings, so nothing the model writes can become
// markup. Links are only made from http(s) URLs.

function inline(text: string, keyPrefix: string): ReactNode[] {
  const out: ReactNode[] = [];
  const re = /(`[^`\n]+`)|\*\*([^*\n]+)\*\*|\[([^\]\n]+)\]\((https?:\/\/[^\s)]+)\)|(https?:\/\/[^\s<>)\]]+[^\s<>)\].,;:!?])/g;
  let last = 0;
  let m: RegExpExecArray | null;
  let n = 0;
  while ((m = re.exec(text)) !== null) {
    if (m.index > last) out.push(text.slice(last, m.index));
    const key = `${keyPrefix}-${n++}`;
    if (m[1]) out.push(<code key={key}>{m[1].slice(1, -1)}</code>);
    else if (m[2]) out.push(<strong key={key}>{m[2]}</strong>);
    else if (m[3] && m[4])
      out.push(
        <a key={key} href={m[4]} target="_blank" rel="noopener noreferrer nofollow">
          {m[3]}
        </a>,
      );
    else if (m[5])
      out.push(
        <a key={key} href={m[5]} target="_blank" rel="noopener noreferrer nofollow">
          {m[5]}
        </a>,
      );
    last = m.index + m[0].length;
  }
  if (last < text.length) out.push(text.slice(last));
  return out;
}

export function Markdown({ text }: { text: string }) {
  const blocks: ReactNode[] = [];
  const lines = text.replace(/\r\n/g, "\n").split("\n");
  let i = 0;
  let key = 0;

  while (i < lines.length) {
    const line = lines[i]!;

    if (line.trim() === "") {
      i++;
      continue;
    }

    if (line.trimStart().startsWith("```")) {
      const body: string[] = [];
      i++;
      while (i < lines.length && !lines[i]!.trimStart().startsWith("```")) body.push(lines[i++]!);
      i++; // closing fence
      blocks.push(
        <pre key={key++} className="md-pre">
          <code>{body.join("\n")}</code>
        </pre>,
      );
      continue;
    }

    if (/^\s*([-*]|\d+[.)])\s+/.test(line)) {
      const ordered = /^\s*\d+[.)]\s+/.test(line);
      const items: string[] = [];
      while (i < lines.length && /^\s*([-*]|\d+[.)])\s+/.test(lines[i]!)) {
        items.push(lines[i]!.replace(/^\s*([-*]|\d+[.)])\s+/, ""));
        i++;
      }
      const List = ordered ? "ol" : "ul";
      blocks.push(
        <List key={key++}>
          {items.map((item, j) => (
            <li key={j}>{inline(item, `li${key}-${j}`)}</li>
          ))}
        </List>,
      );
      continue;
    }

    if (line.startsWith(">")) {
      const quote: string[] = [];
      while (i < lines.length && lines[i]!.startsWith(">")) quote.push(lines[i++]!.replace(/^>\s?/, ""));
      blocks.push(<blockquote key={key++}>{inline(quote.join(" "), `q${key}`)}</blockquote>);
      continue;
    }

    const heading = /^(#{1,4})\s+(.*)$/.exec(line);
    if (heading) {
      blocks.push(
        <p key={key++} className="md-heading">
          {inline(heading[2]!, `h${key}`)}
        </p>,
      );
      i++;
      continue;
    }

    // A paragraph runs until a blank line or the start of another block.
    const para: string[] = [];
    while (
      i < lines.length &&
      lines[i]!.trim() !== "" &&
      !lines[i]!.trimStart().startsWith("```") &&
      !/^\s*([-*]|\d+[.)])\s+/.test(lines[i]!) &&
      !lines[i]!.startsWith(">")
    ) {
      para.push(lines[i++]!);
    }
    const joined = para.join(" ");
    // The agent's system prompt asks it to end with this line; set it apart.
    // The model sometimes writes it in bold or changes the capitals.
    const how = /^\**\s*how i did it\s*:?\s*\**\s*:?\s*/i.exec(joined);
    if (how) {
      blocks.push(
        <p key={key++} className="md-how">
          <span className="eli5-tag">how I did it</span>
          {inline(joined.slice(how[0].length).trim(), `how${key}`)}
        </p>,
      );
    } else {
      blocks.push(<p key={key++}>{inline(joined, `p${key}`)}</p>);
    }
  }

  return <div className="md">{blocks}</div>;
}
