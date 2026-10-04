import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { test } from "node:test";
import { diffLines, findHunks, splitLines } from "../src/lib/diff.ts";
import { highlight, langFor } from "../src/lib/highlight.ts";
import { buildOutline, declName, describePlacement, parseDecls } from "../src/lib/outline.ts";
import { SSEParser } from "../src/lib/sse.ts";

const guide = JSON.parse(readFileSync(new URL("../src/generated/guide.json", import.meta.url), "utf8")) as {
  steps: { id: string; code: string }[];
};

test("diff: applying the diff to the old file gives the new file", () => {
  for (let i = 1; i < guide.steps.length; i++) {
    const oldLines = splitLines(guide.steps[i - 1]!.code);
    const newLines = splitLines(guide.steps[i]!.code);
    const diff = diffLines(oldLines, newLines);
    assert.deepEqual(diff.filter((d) => d.kind !== "add").map((d) => d.text), oldLines, `old side of ${guide.steps[i]!.id}`);
    assert.deepEqual(diff.filter((d) => d.kind !== "del").map((d) => d.text), newLines, `new side of ${guide.steps[i]!.id}`);
    // Line numbers must be consecutive on each side.
    assert.deepEqual(diff.filter((d) => d.newNo).map((d) => d.newNo), newLines.map((_, n) => n + 1));
    assert.deepEqual(diff.filter((d) => d.oldNo).map((d) => d.oldNo), oldLines.map((_, n) => n + 1));
  }
});

test("diff: small cases", () => {
  assert.deepEqual(diffLines([], ["a"]).map((d) => d.kind), ["add"]);
  assert.deepEqual(diffLines(["a"], []).map((d) => d.kind), ["del"]);
  assert.deepEqual(diffLines(["a", "b", "c"], ["a", "x", "c"]).map((d) => d.kind + d.text), ["samea", "delb", "addx", "samec"]);
});

test("hunks: ranges point at the right new-file lines", () => {
  const hunks = findHunks(diffLines(["a", "b", "c", "d"], ["a", "X", "Y", "b", "c", "d", "Z"]));
  assert.deepEqual(hunks.map((h) => [h.start, h.end, h.added, h.removed]), [
    [2, 3, 2, 0],
    [7, 7, 1, 0],
  ]);
  // A pure removal sits before the next surviving line.
  const removal = findHunks(diffLines(["a", "b", "c"], ["a", "c"]));
  assert.deepEqual(removal.map((h) => [h.start, h.end, h.added, h.removed]), [[2, 2, 0, 1]]);
});

test("outline: names declarations the way Go spells them", () => {
  assert.equal(declName("func main() {"), "func main");
  assert.equal(declName("func (a *Agent) Run(ctx context.Context) error {"), "func (a *Agent) Run");
  assert.equal(declName("func GenerateSchema[T any]() anthropic.ToolInputSchemaParam {"), "func GenerateSchema");
  assert.equal(declName("type Agent struct {"), "type Agent");
  assert.equal(declName("var ReadFileDefinition = ToolDefinition{"), "var ReadFileDefinition");
  assert.equal(declName("import ("), "import");
});

test("placement: every change in every step lands somewhere a beginner can find", () => {
  const seen = new Set<string>();
  for (let i = 1; i < guide.steps.length; i++) {
    const oldLines = splitLines(guide.steps[i - 1]!.code);
    const newLines = splitLines(guide.steps[i]!.code);
    const diff = diffLines(oldLines, newLines);
    const added = new Set(diff.filter((d) => d.kind === "add").map((d) => d.newNo!));
    const existing = new Set(parseDecls(oldLines).map((d) => d.name));
    for (const h of findHunks(diff)) {
      const label = describePlacement(newLines, h.start, h.end, added, existing).label;
      assert.match(label, /^(Inside|New block|In the import list)/, `${guide.steps[i]!.id} lines ${h.start}-${h.end}: ${label}`);
      seen.add(`${guide.steps[i]!.id}: ${label}`);
    }
  }
  // Spot checks against the tutorial's actual structure.
  assert.ok(seen.has("02-tool-definitions: Inside type Agent"));
  assert.ok(seen.has("02-tool-definitions: Inside func main"));
  // NewAgent gains a parameter: its first line is retyped, but it is not a new function.
  assert.ok(seen.has("02-tool-definitions: Inside func NewAgent"));
  assert.ok(![...seen].some((s) => s.startsWith("02-tool-definitions: New block, below type Agent")));
  assert.ok(seen.has("02-tool-definitions: Inside func (a *Agent) runInference"));
  assert.ok(seen.has("04-tool-loop: Inside func (a *Agent) Run"));
  assert.ok(seen.has("04-tool-loop: New block, below func (a *Agent) Run"));
  assert.ok([...seen].some((s) => s.startsWith("06-edit-file: New block at the end of the file")));
  assert.ok(seen.has("05-list-files: In the import list at the top"));
});

test("outline: knows which step adds and edits each part of the finished file", () => {
  const rows = buildOutline(guide.steps.map((s) => s.code));
  const by = Object.fromEntries(rows.map((r) => [r.name, r]));
  assert.equal(by["func main"]!.introduced, 0);
  assert.deepEqual(by["func main"]!.changed, [1, 2, 4, 5]);
  assert.equal(by["func (a *Agent) Run"]!.introduced, 0);
  assert.deepEqual(by["func (a *Agent) Run"]!.changed, [3]);
  assert.equal(by["func (a *Agent) executeTool"]!.introduced, 3);
  assert.equal(by["type ToolDefinition"]!.introduced, 1);
  assert.equal(by["func ReadFile"]!.introduced, 2);
  assert.equal(by["func ListFiles"]!.introduced, 4);
  assert.equal(by["func EditFile"]!.introduced, 5);
  // Ranges cover the file without overlap and in order.
  const decls = parseDecls(splitLines(guide.steps.at(-1)!.code));
  for (let i = 1; i < decls.length; i++) assert.ok(decls[i]!.start > decls[i - 1]!.end);
});

test("highlight: one token list per line, text preserved exactly", () => {
  for (const step of guide.steps) {
    const code = step.code.replace(/\n$/, "");
    const lines = highlight(code, "go");
    assert.equal(lines.map((l) => l.map((t) => t.text).join("")).join("\n"), code, step.id);
  }
  const js = "const a = `multi\nline`; // done\nfunction f() { return 'x'; }";
  const out = highlight(js, "js");
  assert.equal(out.length, 3);
  assert.equal(out.map((l) => l.map((t) => t.text).join("")).join("\n"), js);
  assert.equal(out[1]!.at(-1)!.kind, "com");
  assert.equal(out[2]![0]!.kind, "kw");
  assert.equal(langFor("main.go"), "go");
  assert.equal(langFor("notes.md"), "text");
});

test("highlight: a multi-line raw string stays a string on every line", () => {
  const go = "var d = `first\n\nthird`\nx := 1";
  const out = highlight(go, "go");
  assert.equal(out.length, 4);
  assert.equal(out[1]!.length, 0); // the empty line inside the string
  assert.equal(out[2]![0]!.kind, "str");
  assert.equal(out[3]!.some((t) => t.kind === "num"), true);
});

test("sse: messages split across chunks, comments ignored", () => {
  const p = new SSEParser();
  assert.deepEqual(p.feed("event: text_delta\ndata: {\"te"), []);
  assert.deepEqual(p.feed('xt":"hi"}\n\n: keep-alive\n\nevent: done\n'), [{ event: "text_delta", data: '{"text":"hi"}' }]);
  assert.deepEqual(p.feed("data: {}\n\n"), [{ event: "done", data: "{}" }]);
  assert.deepEqual(new SSEParser().feed("data: a\r\ndata: b\r\n\r\n"), [{ event: "message", data: "a\nb" }]);
});
