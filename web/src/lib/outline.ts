// Answers the question the tutorial left open for a beginner: "where in
// main.go does this code go?" Given a Go file and a range of lines, it names
// the surrounding declaration in words.

const TOP_LEVEL = /^(func|type|var|const|import)\b/;

/** The name of a top-level Go declaration, spelled the way it is in the file. */
export function declName(line: string): string {
  let m = /^func\s+(\([^)]*\)\s*)?([A-Za-z_]\w*)/.exec(line);
  if (m) return `func ${m[1] ?? ""}${m[2]}`.replace(/\s+/g, " ");
  m = /^(type|var|const)\s+([A-Za-z_]\w*)/.exec(line);
  if (m) return `${m[1]} ${m[2]}`;
  if (line.startsWith("import")) return "import";
  return line.trim();
}

export type Placement = {
  /** "inside" an existing declaration, a "new" declaration, or the "imports". */
  kind: "inside" | "new" | "imports";
  /** A sentence fragment: "Inside func main", "New block, below type Agent". */
  label: string;
};

/**
 * Describes where lines start..end (1-based, inclusive) of a Go file sit.
 * `added` says which line numbers are new, so "below X" can name a
 * declaration the reader already has rather than one arriving in the same step.
 */
export function describePlacement(
  lines: string[],
  start: number,
  end: number,
  added?: Set<number>,
  /** Names of declarations that already existed before this step. */
  existing?: Set<string>,
): Placement {
  // The first line in the range with something on it decides the kind.
  let first = start;
  while (first < end && lines[first - 1]!.trim() === "") first++;
  const firstLine = lines[first - 1] ?? "";

  const previousDecl = (from: number, existingOnly: boolean): string | null => {
    for (let n = from; n >= 1; n--) {
      if (TOP_LEVEL.test(lines[n - 1]!) && !(existingOnly && added?.has(n))) return lines[n - 1]!;
    }
    return null;
  };

  if (TOP_LEVEL.test(firstLine) && !firstLine.startsWith("import")) {
    // Retyping the first line of something the reader already has is an
    // edit to that declaration, not a new one.
    if (existing?.has(declName(firstLine))) return { kind: "inside", label: `Inside ${declName(firstLine)}` };
    const above = previousDecl(first - 1, true);
    if (!above) return { kind: "new", label: "New block at the top of the file, below the imports" };
    if (above.startsWith("import")) return { kind: "new", label: "New block, right below the imports" };
    const isLast = !lines.slice(end).some((l) => TOP_LEVEL.test(l));
    return {
      kind: "new",
      label: isLast ? `New block at the end of the file, below ${declName(above)}` : `New block, below ${declName(above)}`,
    };
  }

  const enclosing = previousDecl(first, false);
  if (!enclosing) return { kind: "inside", label: "At the top of the file" };
  if (enclosing.startsWith("import")) return { kind: "imports", label: "In the import list at the top" };
  return { kind: "inside", label: `Inside ${declName(enclosing)}` };
}

export type Decl = { name: string; start: number; end: number; body: string };

/** Splits a Go file into its top-level declarations. */
export function parseDecls(lines: string[]): Decl[] {
  const decls: Decl[] = [];
  lines.forEach((line, i) => {
    if (TOP_LEVEL.test(line)) decls.push({ name: declName(line), start: i + 1, end: i + 1, body: "" });
  });
  decls.forEach((d, i) => {
    let end = (decls[i + 1]?.start ?? lines.length + 1) - 1;
    while (end > d.start && lines[end - 1]!.trim() === "") end--;
    d.end = end;
    d.body = lines.slice(d.start - 1, end).join("\n");
  });
  return decls;
}

export type OutlineRow = {
  name: string;
  /** Line range in the finished file. */
  start: number;
  end: number;
  /** Index of the checkpoint that first contains this declaration. */
  introduced: number;
  /** Indexes of later checkpoints that change it. */
  changed: number[];
};

/**
 * For every declaration in the finished file: which step adds it, and which
 * later steps touch it. This is the map of the whole build.
 */
export function buildOutline(checkpoints: string[]): OutlineRow[] {
  const parsed = checkpoints.map((code) => parseDecls(code.replace(/\n$/, "").split("\n")));
  const final = parsed[parsed.length - 1] ?? [];
  return final.map((decl) => {
    let introduced = -1;
    const changed: number[] = [];
    let previous: string | undefined;
    parsed.forEach((decls, i) => {
      const found = decls.find((d) => d.name === decl.name);
      if (!found) return;
      if (introduced < 0) introduced = i;
      else if (previous !== undefined && found.body !== previous) changed.push(i);
      previous = found.body;
    });
    return { name: decl.name, start: decl.start, end: decl.end, introduced, changed };
  });
}
