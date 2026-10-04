// Runs a JavaScript file from the workspace in the visitor's own browser.
//
// The agent has no way to execute code, on purpose: running model-written
// code on the server is the riskiest thing an agent can do. But the
// tutorial's demos end with `node fizzbuzz.js`, and seeing the output is half
// the point. So the code runs here instead, in a Web Worker: off the main
// thread, with no DOM, with the network APIs removed, and killed after a
// timeout. Removing globals is tidiness, not a wall; the wall is the page's
// Content-Security-Policy, which the worker inherits and which only allows
// connections to this site and its API. The only person the code can affect
// is the visitor who pressed Run on code their own session produced.

export type RunLine = { level: "log" | "error" | "warn"; text: string };
export type RunResult = { lines: RunLine[]; timedOut: boolean };

const PRELUDE = `
"use strict";
const __post = self.postMessage.bind(self);
const __fmt = (v) => {
  if (typeof v === "string") return v;
  if (v instanceof Error) return v.stack || String(v);
  try { return JSON.stringify(v); } catch { return String(v); }
};
const __emit = (level) => (...args) => __post({ type: "line", level, text: args.map(__fmt).join(" ") });
self.console = { log: __emit("log"), info: __emit("log"), debug: __emit("log"), warn: __emit("warn"), error: __emit("error") };
// No network and no loading of further code.
for (const name of ["fetch", "XMLHttpRequest", "WebSocket", "WebTransport", "EventSource", "importScripts", "Worker", "SharedWorker", "indexedDB", "caches", "BroadcastChannel"]) {
  try { Object.defineProperty(self, name, { value: undefined, configurable: false, writable: false }); } catch {}
}
// Enough of Node for simple scripts to run.
self.process = { argv: ["node", "script.js"], env: {}, exit: () => { throw new Error("process.exit() called"); } };
self.addEventListener("error", (e) => { __post({ type: "line", level: "error", text: String(e.message || e) }); });
self.addEventListener("unhandledrejection", (e) => { __post({ type: "line", level: "error", text: "Unhandled rejection: " + __fmt(e.reason) }); });
`;

const MAX_LINES = 300;

export function runJavaScript(code: string, timeoutMs = 3000): Promise<RunResult> {
  return new Promise((resolve) => {
    const source = `${PRELUDE}\ntry {\n${code}\n} catch (e) { console.error(e && e.name ? e.name + ": " + e.message : String(e)); }\n__post({ type: "done" });\n`;
    const url = URL.createObjectURL(new Blob([source], { type: "text/javascript" }));
    const lines: RunLine[] = [];
    let settled = false;
    let worker: Worker;

    const finish = (timedOut: boolean) => {
      if (settled) return;
      settled = true;
      clearTimeout(timer);
      worker?.terminate();
      URL.revokeObjectURL(url);
      resolve({ lines, timedOut });
    };
    const timer = setTimeout(() => finish(true), timeoutMs);

    try {
      worker = new Worker(url);
    } catch (e) {
      lines.push({ level: "error", text: "This browser blocked the sandboxed runner: " + String(e) });
      finish(false);
      return;
    }
    worker.onmessage = (e: MessageEvent<{ type: string; level?: RunLine["level"]; text?: string }>) => {
      if (e.data.type === "line" && lines.length < MAX_LINES) {
        lines.push({ level: e.data.level ?? "log", text: (e.data.text ?? "").slice(0, 2000) });
        if (lines.length === MAX_LINES) lines.push({ level: "warn", text: `Output stopped after ${MAX_LINES} lines.` });
      }
      // Leave a moment for timers the script may have set, then stop.
      if (e.data.type === "done") setTimeout(() => finish(false), 150);
    };
    worker.onerror = (e) => {
      // A syntax error prevents the worker script from starting at all.
      lines.push({ level: "error", text: e.message || "The script could not be parsed." });
      e.preventDefault();
      finish(false);
    };
  });
}
