# ADR 0001: Security comes from what the agent can reach, not from what it is told

Status: accepted

## Context

The tutorial agent has three tools and no boundary. The model chooses every
path, and the tools pass it to `os.ReadFile` and `os.WriteFile`. Anything the
operating-system user can touch, the agent can touch.

The obvious fix is a system prompt: "only work inside the project folder,
never read secrets". Prompts are instructions to a model, and a model can be
argued with. A file the agent reads, a web page it searches, or the user
themselves can supply text that outweighs the system prompt. Published
jailbreaks change faster than any filter list.

## Decision

Treat prompt-level rules as helpful and unreliable. Enforce every property
that matters in Go, below the model:

- Tools only see a `workspace.FS`. On the web that is a per-session in-memory
  map with quotas. In the terminal it is a directory opened with `os.Root`,
  so the kernel refuses `..`, absolute paths and escaping symlinks.
- Paths are validated in one function, `workspace.Clean`, which rejects
  rather than repairs.
- There is no tool that executes code or makes network requests.
- Cost is bounded by counters in the server, not by asking the model to be
  brief.

The system prompt still says the same things, because a model that knows the
rules wastes fewer rounds bumping into them. It is not counted as a control.

## Alternatives considered

- **Prompt rules plus an input filter.** Cheap, and the first thing an
  attacker tests. Rejected as the primary control; kept as telemetry
  (ADR 0005).
- **A second model that reviews each tool call.** Adds cost and latency to
  every call and is itself promptable. Not needed when the tools cannot do
  harm in the first place.

## Consequences

- A successful prompt injection can make the agent edit files in the
  attacker's own sandbox, and nothing else. That is the worst case and it is
  acceptable.
- The safety claims are testable without a model. `evals/01-*.json` calls the
  tools directly with hostile input; a pass holds for every model and prompt.
- The agent is less capable than one with a shell. That trade is deliberate
  for a public demo.
