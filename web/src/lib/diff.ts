// Line diff used by the build guide (what changed between two checkpoints)
// and by the playground (what an edit changed in a file).

export type DiffLine = {
  kind: "same" | "add" | "del";
  text: string;
  /** 1-based line number in the old file (same, del). */
  oldNo?: number;
  /** 1-based line number in the new file (same, add). */
  newNo?: number;
};

/** A run of consecutive added and removed lines. */
export type Hunk = {
  /** Index of the hunk's first line in the diff array. */
  index: number;
  /** First and last new-file line numbers the hunk touches. For a hunk
   *  that only removes lines, both are the line the removal sits before. */
  start: number;
  end: number;
  added: number;
  removed: number;
};

/**
 * Longest-common-subsequence diff. The files here are a few hundred lines,
 * so the plain O(n·m) table is fast and has no edge cases to explain.
 */
export function diffLines(oldLines: string[], newLines: string[]): DiffLine[] {
  const n = oldLines.length;
  const m = newLines.length;
  const width = m + 1;
  const table = new Uint32Array((n + 1) * width);
  for (let i = n - 1; i >= 0; i--) {
    for (let j = m - 1; j >= 0; j--) {
      table[i * width + j] =
        oldLines[i] === newLines[j]
          ? table[(i + 1) * width + j + 1]! + 1
          : Math.max(table[(i + 1) * width + j]!, table[i * width + j + 1]!);
    }
  }

  const out: DiffLine[] = [];
  let i = 0;
  let j = 0;
  while (i < n && j < m) {
    if (oldLines[i] === newLines[j]) {
      out.push({ kind: "same", text: newLines[j]!, oldNo: i + 1, newNo: j + 1 });
      i++;
      j++;
    } else if (table[(i + 1) * width + j]! >= table[i * width + j + 1]!) {
      out.push({ kind: "del", text: oldLines[i]!, oldNo: i + 1 });
      i++;
    } else {
      out.push({ kind: "add", text: newLines[j]!, newNo: j + 1 });
      j++;
    }
  }
  for (; i < n; i++) out.push({ kind: "del", text: oldLines[i]!, oldNo: i + 1 });
  for (; j < m; j++) out.push({ kind: "add", text: newLines[j]!, newNo: j + 1 });
  return out;
}

/** Groups a diff into hunks. */
export function findHunks(diff: DiffLine[]): Hunk[] {
  const hunks: Hunk[] = [];
  let current: Hunk | null = null;
  let nextNew = 1; // new-file line number of the next unchanged or added line
  diff.forEach((line, index) => {
    if (line.kind === "same") {
      current = null;
      nextNew = line.newNo! + 1;
      return;
    }
    if (!current) {
      current = { index, start: nextNew, end: nextNew, added: 0, removed: 0 };
      hunks.push(current);
    }
    if (line.kind === "add") {
      current.added++;
      current.end = line.newNo!;
      nextNew = line.newNo! + 1;
    } else {
      current.removed++;
    }
  });
  return hunks;
}

export function splitLines(text: string): string[] {
  const lines = text.split("\n");
  if (lines.length > 0 && lines[lines.length - 1] === "") lines.pop();
  return lines;
}
