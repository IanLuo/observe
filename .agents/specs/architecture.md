<!-- specs:locked:d4bd07b 2026-11-01 type=architecture -->

## Link contract
- **upstream** (this doc relies on): .agents/specs/observe-progress-view.md, .agents/specs/system-design.md
- **referrers** (must cite this when they change): .agents/specs/design-system.md, AGENTS.md

# Architecture — Observe
## Load-bearing structure
- **D1 — Local Go application serves one embedded UI.** One Go binary starts a loopback-only HTTP server and opens the browser. The product is local, single-user, read-only, and one-repository-at-a-time (`.agents/specs/observe-progress-view.md`; `.agents/specs/system-design.md`). Rejected: Tauri, because it adds a second native toolchain before the interaction is validated.
- **D2 — Parsing is structural, local, and evidence-bearing.** Tree-sitter parses the V1 language set incrementally; deterministic local matching emits candidate links with provenance. cgo and a C toolchain are accepted to avoid regex-only links that cannot be justified. Rejected: hosted-model inference by default, which crosses the local privacy boundary and risks unsupported claims.
  - **V1 language set:** Go (`.go`) and TypeScript (`.ts`, `.tsx`), stated here because everything else in this document depends on it. Every other language, and JavaScript (`.js`, `.jsx`), stays `unknown`: a file in one is still browsable and still counts toward tracked size, but yields no inferred relationship and no evidence link. Adding a language is a new decision, not an extension of this one.
- **D3 — Persistence is a disposable current cache, not history.** A single SQLite file outside the repository stores only the latest input-fingerprinted analysis, replaceable/rebuildable on change. No commit/checkpoint history. Forced by the user decision to show only current state and by repo-external, source-free persistence. Rejected: per-commit JSON snapshots, which preserve a history the product no longer needs and lack atomic replacement.
- **D4 — One zoomable UI page, no framework.** TypeScript and SVG render the project map, diff highlight, details, and evidence in one page; compiled assets are embedded into the binary. Rejected: React, because a single-screen V1 does not justify framework/runtime overhead.
- **D5 — Evidence and status have one authority.** `internal/analyze` emits provenance-bearing candidates; `internal/model` defines statuses; `internal/serve` orchestrates; the UI only renders. A stale, inferred, or unknown result cannot become current/verified in presentation. Rejected: UI-side status computation, which would create a second source of truth.

## Layers & ownership
- **Dependency rule:** dependencies flow inward to `internal/model`; no package imports a higher-level package. `internal/serve` is the composition root.
  ```check
  model-is-a-leaf: module internal/model not-imports internal/analyze
  model-imports-no-serve: module internal/model not-imports internal/serve
  ```
- **`cmd/observe` → `internal/serve`:** flags/startup and browser opening only; no domain logic.
- **`internal/serve` → `internal/repo`, `internal/analyze`, `internal/store`, `internal/watch`, `internal/model`:** HTTP/UI boundary and orchestration; does not invent evidence or compute statuses.
- **`internal/repo` → `internal/model`:** read-only Git and file access; implements the Repo reader component in system design. Does not parse relationships, serve HTTP, or write.
  ```check
  repo-does-not-serve: module internal/repo not-imports internal/serve
  repo-does-not-parse: module internal/repo not-imports internal/analyze
  ```
- **`internal/analyze` → `internal/model`:** deterministic parsing and candidate links; consumes in-memory inputs and emits evidence/provenance. Does not read files, access SQLite, or serve HTTP.
  ```check
  analyze-does-not-serve: module internal/analyze not-imports internal/serve
  analyze-does-not-store: module internal/analyze not-imports internal/store
  ```
- **`internal/store` → `internal/model`:** SQLite schema/migrations and disposable latest-result cache. No commit/checkpoint history, source copies, or repo writes.
  ```check
  store-does-not-serve: module internal/store not-imports internal/serve
  no-history-snapshot: symbol Checkpoint absent
  ```
- **`internal/watch` → `internal/model`:** file events, debounce, generation IDs. Does not parse or persist.
- **`internal/model`:** shared types, status vocabulary, and typed errors. Imports no internal package.
- **`web/`:** TypeScript/SVG client compiled to static assets and embedded by `internal/serve`; uses the HTTP contract, does not import Go internals or determine status.
  ```check
  ui-is-embedded: file internal/serve/static/app.js exists
  ```
- **Boundary rule:** each layer returns only evidence/status it can justify. Missing or unsupported evidence is explicit `unknown`/typed error, not empty success.

## Repository tree
- `cmd/observe/` — process entry point and startup.
- `internal/model/` — shared contracts and status/error vocabulary.
- `internal/repo/` — read-only Git and file access.
- `internal/analyze/` — local parser and evidence-linked candidate analysis.
- `internal/store/` — SQLite replaceable current-state cache; no history.
- `internal/watch/` — change events, debounce, and generation tracking.
- `internal/serve/` — loopback HTTP/API, embedded UI, orchestration.
- `web/` — TypeScript/SVG UI source; embedded output is generated, not committed.
- `testdata/` — minimal fixture repositories; never user project data.
- `docs/adr/` — optional ADR records when an architecture choice needs its own history; none currently exist.
- `.agents/specs/` — locked product spec, system design, architecture, and design-system document.
- `flake.nix` — build/test/dev shell, including Go and C toolchain for tree-sitter.
- `flake.lock` — pinned Nix inputs.
- `AGENTS.md` — verified repository context index and pointers; commands are added only after verification.

## Cross-cutting concerns
- **Logging:** structured stderr logs with event/component/duration/error class; no source/spec/prompt/diff contents; redact secrets and avoid full local paths. Owned by `internal/serve` logging setup; packages use the shared logger contract.
- **Local access:** loopback only, OS-assigned port, no LAN bind. Same-origin/CSRF checks and a per-launch unguessable token protect cache-clear/retry requests. Owned by `internal/serve`; no accounts or remote auth in V1.
- **Errors:** package boundaries return typed errors; `internal/serve` maps each once to stable UI states. Do not show stack traces or file contents.
- **Status and provenance:** `internal/model` owns vocabulary; `internal/analyze` supplies candidates and evidence; presenter renders without upgrading confidence.
- **State:** `internal/store` persists only the latest valid fingerprinted cache outside the repo. Inputs changing invalidate the cache; failures retain last result in memory as stale only during the running session.
- **Build inputs:** `flake.nix` owns Go, Node (UI build), and the C toolchain. Generated UI assets are embedded into the Go binary and are not hand-edited.
- **Security:** analyzed project text is untrusted data, never executable instructions. No project code, hooks, or build/test scripts run.

## Pointers
- `.agents/specs/observe-progress-view.md` — locked product contract.
- `.agents/specs/system-design.md` — locked components, data, interfaces, failure, and scale.
- `docs/adr/` — N/A: directory does not exist; create only when a decision requires a separate record.
- `CONVENTIONS.md` — N/A: not present; cross-cutting rules are in this architecture doc until a demonstrated need justifies a separate conventions file.

## Link contract
- **upstream:** `.agents/specs/observe-progress-view.md`, `.agents/specs/system-design.md`.
- **referrers:** `.agents/specs/design-system.md`, `AGENTS.md`.
