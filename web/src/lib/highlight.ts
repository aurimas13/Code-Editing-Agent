// A small tokenizer for Go and JavaScript. It exists so code can be coloured
// line by line (the guide highlights individual added lines) without pulling
// a highlighting library into the bundle.

export type TokenKind = "kw" | "str" | "com" | "num" | "fn" | "type" | "plain";
export type Token = { kind: TokenKind; text: string };
export type Lang = "go" | "js" | "text";

const GO_KEYWORDS = new Set(
  "break case chan const continue default defer else fallthrough for func go goto if import interface map package range return select struct switch type var nil true false".split(" "),
);
const GO_TYPES = new Set("string int int64 int32 float64 bool byte rune error any uint uint64".split(" "));
const JS_KEYWORDS = new Set(
  "break case catch class const continue default delete do else export extends finally for from function if import in instanceof let new of return static super switch this throw try typeof var void while yield async await null undefined true false".split(" "),
);

export function langFor(path: string): Lang {
  if (path.endsWith(".go")) return "go";
  if (/\.(js|mjs|cjs|ts|tsx|jsx)$/.test(path)) return "js";
  return "text";
}

/** Tokenizes a whole file and returns one token list per line. */
export function highlight(code: string, lang: Lang): Token[][] {
  const lines: Token[][] = [[]];
  const push = (kind: TokenKind, text: string) => {
    // A token may span lines (block comments, raw strings): split it.
    const parts = text.split("\n");
    parts.forEach((part, i) => {
      if (i > 0) lines.push([]);
      if (part !== "") lines[lines.length - 1]!.push({ kind, text: part });
    });
  };
  if (lang === "text") {
    push("plain", code);
    return lines;
  }

  const keywords = lang === "go" ? GO_KEYWORDS : JS_KEYWORDS;
  const n = code.length;
  let i = 0;
  while (i < n) {
    const c = code[i]!;
    const two = code.slice(i, i + 2);

    if (two === "//") {
      const end = code.indexOf("\n", i);
      const stop = end < 0 ? n : end;
      push("com", code.slice(i, stop));
      i = stop;
    } else if (two === "/*") {
      const end = code.indexOf("*/", i + 2);
      const stop = end < 0 ? n : end + 2;
      push("com", code.slice(i, stop));
      i = stop;
    } else if (c === "`") {
      const end = code.indexOf("`", i + 1);
      const stop = end < 0 ? n : end + 1;
      push("str", code.slice(i, stop));
      i = stop;
    } else if (c === '"' || c === "'") {
      let j = i + 1;
      while (j < n && code[j] !== c && code[j] !== "\n") j += code[j] === "\\" ? 2 : 1;
      const stop = Math.min(j + 1, n);
      push("str", code.slice(i, stop));
      i = stop;
    } else if (/[0-9]/.test(c)) {
      let j = i + 1;
      while (j < n && /[0-9a-fA-FxX_.]/.test(code[j]!)) j++;
      push("num", code.slice(i, j));
      i = j;
    } else if (/[A-Za-z_$]/.test(c)) {
      let j = i + 1;
      while (j < n && /[\w$]/.test(code[j]!)) j++;
      const word = code.slice(i, j);
      let kind: TokenKind = "plain";
      if (keywords.has(word)) kind = "kw";
      else if (lang === "go" && GO_TYPES.has(word)) kind = "type";
      else if (code[j] === "(") kind = "fn";
      push(kind, word);
      i = j;
    } else {
      // Punctuation and whitespace: gather a run so the DOM stays small.
      let j = i + 1;
      while (j < n && !/[A-Za-z0-9_$"'`/]/.test(code[j]!)) j++;
      push("plain", code.slice(i, j));
      i = j;
    }
  }
  return lines;
}
