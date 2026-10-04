# ADR 0002: An in-memory workspace per visitor instead of containers

Status: accepted

## Context

A public demo needs every visitor's agent isolated from the host and from
other visitors. The usual answer is a container or microVM per session.

This agent has three tools: read a text file, list files, replace text in a
file. None of them runs anything.

## Decision

Give each session a `workspace.MemFS`: a mutex-guarded `map[string][]byte`
with limits on file count, file size and total size. Seed it with five small
files. Snapshot it into the session row so it survives a restart.

Do not execute code on the server at all. Where a demo wants to show a
script's output, run it in the visitor's own browser, in a Web Worker with
network access removed and a three-second timeout (`web/src/lib/runner.ts`).

## Alternatives considered

- **Container per session** (Firecracker, gVisor, a Docker sidecar). Real
  isolation for real execution, and the right answer for an agent with a
  shell. For three text tools it is cold-start latency, an orchestration
  layer and an idle cost, bought to protect a filesystem the agent does not
  need.
- **A temp directory per session on the server's disk, confined with
  `os.Root`.** Works, and is what the terminal agent does. On a shared
  server it adds cleanup, disk quotas and a class of bugs (symlinks, races)
  that a map does not have.

## Consequences

- Isolation is a language-level property: one session's map is unreachable
  from another's. `TestSessionsAreIsolated` checks it over HTTP.
- Sessions cost a few kilobytes and start instantly.
- The agent cannot run tests or builds. For this project that is the point;
  an agent that needs to would need the container design, and `workspace.FS`
  is the seam where it would plug in.
- Workspaces live in one process's memory. See "What this does not do" in
  the README for what multi-instance would take.
