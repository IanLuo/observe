# AGENTS.md — Observe

## Intent

- Product goal and acceptance contract: `.agents/specs/observe-progress-view.md` (locked).
- System boundaries, data, interfaces, failure, and scale: `.agents/specs/system-design.md` (locked).
- Repository structure and load-bearing rationale: `.agents/specs/architecture.md` (to be locked).
- Visual tokens and interface states: `.agents/specs/design-system.md` (locked).

## How to run / build / test

- N/A: implementation has not started; add verified commands when the first implementation lands.

## Hot invariants

- The product spec and system design are the source of truth for requirements and component boundaries.
- Read-only local operation. Never execute repository code, hooks, or build scripts from an analyzed project.
- Locked specs are the goal baseline. Task prompts and other docs are context only.
- Analyzer evidence must carry provenance. Unknown stays unknown; code presence alone is not verification.
- Persistent state is only a replaceable current-analysis cache outside the repository. No history snapshots.
- All application access binds to loopback only; no LAN exposure.

## Architecture elevator

```text
cmd/observe
    └── internal/serve  ──→  internal/repo
          │               ├→ internal/analyze ──→ internal/model
          ├────────────────→ internal/store  ───→ internal/model
          └── internal/watch ──────────────────→ internal/model

web/ (TypeScript + SVG) ── built static assets embedded by internal/serve
```

- `cmd/observe/` — process entry point and startup.
- `internal/model/` — shared snapshot and status contract.
- `internal/repo/` — read-only Git and file access; system-design Repo reader.
- `internal/analyze/` — deterministic parsing and evidence-linked candidate relationships.
- `internal/store/` — replaceable SQLite current-state cache only; no historical snapshots.
- `internal/watch/` — file events, 500 ms debounce, generation tracking.
- `internal/serve/` — loopback HTTP/API and embedded UI; orchestrates pipeline.
- `web/` — TypeScript/SVG UI source; no framework.
- `docs/adr/` — decision records only when a decision requires separate history; none exist yet.
- `testdata/` — small fixture repositories for tests.
- `flake.nix` — build/test/dev environment, including the C toolchain for tree-sitter.
- `.agents/specs/` — locked requirements, system design, architecture, and design system.

## Deeper docs

| When you need… | Read… |
|---|---|
| Product requirements, goals, acceptance and NFRs | `.agents/specs/observe-progress-view.md` |
| Components, data contract, interfaces, failure policy, scale | `.agents/specs/system-design.md` |
| Load-bearing decisions, layer ownership, repo tree | `.agents/specs/architecture.md` |
| UI direction and tokens | `.agents/specs/design-system.md` (locked at `862d5b7`) |
