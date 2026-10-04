// Client for the Go API. Types mirror internal/agent/events.go and
// internal/server on the backend.

import { SSEParser } from "./sse.ts";

export const API_URL = (process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080").replace(/\/$/, "");

export type Mode = "code" | "research";

export type FileView = { path: string; content: string };

export type ToolEvent = {
  id: string;
  name: string;
  input?: unknown;
  output?: string;
  is_error?: boolean;
  duration_ms?: number;
};

export type Guardrail = { kind: string; detail?: string };
export type Source = { url: string; title?: string; cited?: boolean };
export type Usage = { input_tokens: number; output_tokens: number; web_searches: number; cost_usd: number };

export type AgentEvent = {
  type:
    | "model_start"
    | "text_delta"
    | "text"
    | "tool_call"
    | "tool_result"
    | "web_search"
    | "sources"
    | "guardrail"
    | "usage"
    | "done";
  round?: number;
  text?: string;
  tool?: ToolEvent;
  guardrail?: Guardrail;
  sources?: Source[];
  usage?: Usage;
  stop_reason?: string;
};

export type TurnView = {
  mode: Mode;
  input: string;
  reply: string;
  error?: string;
  trace: AgentEvent[];
  usage: Usage;
  sources?: Source[];
};

export type Session = {
  id: string;
  token: string;
  files: FileView[];
  transcript: TurnView[];
  turns_used: number;
  turns_max: number;
};

export type ServerConfig = {
  mode: "live" | "demo";
  model: string;
  research_enabled: boolean;
  store: string;
  tools: string[];
  limits: {
    max_input_chars: number;
    max_output_tokens: number;
    max_rounds: number;
    max_turns_per_session: number;
    turns_per_minute: number;
    turns_per_day: number;
    research_per_day: number;
    web_search_max_uses: number;
  };
  budget: { exhausted: boolean; used_pct: number };
};

export type ResearchAnswer = {
  id: string;
  question: string;
  answer: string;
  sources: Source[];
  model: string;
  created_at: string;
};

export class APIError extends Error {
  constructor(
    public status: number,
    public code: string,
    message: string,
    public retryAfterSeconds?: number,
  ) {
    super(message);
  }
}

async function toError(resp: Response): Promise<APIError> {
  try {
    const body = (await resp.json()) as { error?: { code?: string; message?: string; retry_after_seconds?: number } };
    return new APIError(
      resp.status,
      body.error?.code ?? "unknown",
      body.error?.message ?? `Request failed (${resp.status}).`,
      body.error?.retry_after_seconds,
    );
  } catch {
    return new APIError(resp.status, "unknown", `Request failed (${resp.status}).`);
  }
}

async function request<T>(path: string, init: RequestInit = {}, token?: string): Promise<T> {
  let resp: Response;
  try {
    resp = await fetch(API_URL + path, {
      ...init,
      headers: {
        ...(init.body ? { "Content-Type": "application/json" } : {}),
        ...(token ? { Authorization: `Bearer ${token}` } : {}),
      },
    });
  } catch {
    throw new APIError(0, "network", "Can't reach the agent server. Check that it is running, then try again.");
  }
  if (!resp.ok) throw await toError(resp);
  return (await resp.json()) as T;
}

export const getConfig = () => request<ServerConfig>("/api/config");
export const createSession = () => request<Session>("/api/sessions", { method: "POST" });
export const getSession = (id: string, token: string) =>
  request<Omit<Session, "token">>(`/api/sessions/${id}`, {}, token);
export const resetSession = (id: string, token: string) =>
  request<Omit<Session, "token">>(`/api/sessions/${id}/reset`, { method: "POST" }, token);
export const listResearch = () => request<{ answers: ResearchAnswer[] }>("/api/research");

/** Everything the message stream can deliver, by event name. */
export type StreamHandlers = {
  onTurnStart?: (data: { turn: number; mode: Mode; model: string }) => void;
  onEvent?: (event: AgentEvent) => void;
  /** One file changed during the turn. */
  onFile?: (file: FileView) => void;
  onError?: (message: string) => void;
  onTurnEnd?: (data: {
    turns_used: number;
    turns_max: number;
    usage: Usage;
    rounds: number;
    stop_reason: string;
    latency_ms: number;
    files: FileView[];
  }) => void;
};

/**
 * Sends a message and reads the reply as it streams. Rejections that happen
 * before the stream opens (rate limits, bad input) arrive as an APIError;
 * failures during the turn arrive through onError.
 */
export async function sendMessage(
  session: { id: string; token: string },
  content: string,
  mode: Mode,
  handlers: StreamHandlers,
  signal?: AbortSignal,
): Promise<void> {
  let resp: Response;
  try {
    resp = await fetch(`${API_URL}/api/sessions/${session.id}/messages`, {
      method: "POST",
      headers: { "Content-Type": "application/json", Authorization: `Bearer ${session.token}` },
      body: JSON.stringify({ content, mode }),
      signal,
    });
  } catch {
    throw new APIError(0, "network", "Can't reach the agent server. Check that it is running, then try again.");
  }
  if (!resp.ok || !resp.body) throw await toError(resp);

  const parser = new SSEParser();
  const decoder = new TextDecoder();
  const reader = resp.body.getReader();
  for (;;) {
    const { value, done } = await reader.read();
    if (done) break;
    for (const msg of parser.feed(decoder.decode(value, { stream: true }))) {
      const data = JSON.parse(msg.data);
      switch (msg.event) {
        case "turn_start":
          handlers.onTurnStart?.(data);
          break;
        case "file":
          handlers.onFile?.(data as FileView);
          break;
        case "error":
          handlers.onError?.(data.message);
          break;
        case "turn_end":
          handlers.onTurnEnd?.(data);
          break;
        default:
          handlers.onEvent?.(data as AgentEvent);
      }
    }
  }
}
