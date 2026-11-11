# AGENTS.md — Observe

## Intent

- Product goal and acceptance contract: `.agents/specs/observe-progress-view.md` (locked).
- System boundaries, data, interfaces, failure, and scale: `.agents/specs/system-design.md` (locked).
- Repository structure and load-bearing rationale: `.agents/specs/architecture.md` (locked).
- Visual tokens and interface states: `.agents/specs/design-system.md` (locked).

## How to run / build / test

- `nix develop --command sh -c 'go build ./... && go vet ./... && go test ./... -count=1 && gofmt -l .'` — build, vet, test, format check.
- `nix develop --command go test ./internal/serve -run Browser -v` — drive the page in a real browser: drill-down, refresh on attention, panel resize. Uses `OBSERVE_CHROME` or a local Chrome/Chromium; skips (and says so) when there is none, so `nix build` and CI stay green without one.
- `nix develop --command go test ./internal/serve -run OneHundredThousand -v` — the refresh budget: a generated 100,000-line fixture, an edit, one full refresh timed at p95 (measured 2.15s against a 3s budget). Skips under `-short`.
- `nix build .` — hermetic build; `./result/bin/observe <repository>`.
- `nix develop --command go run ./cmd/observe <repository>` — serve the loopback UI on `127.0.0.1` (prints the URL); `-report` prints the same state as JSON and exits.
- `observe stop [<path>]` — stop the instance serving that project, from any directory inside it. One instance per project; a second start reports the running one instead of shadowing it. The port is the first free one from 8080 unless `-addr` names an exact address.
- `nix develop --command ./web/build.sh` — type-check and bundle `web/` into `internal/serve/static/app.js`. Run it after any `web/` change: the Go build embeds that file, and a test fails when it has drifted from the sources.
- Implemented so far: `internal/model`, `internal/repo`, `internal/analyze` (goal baseline, requirement extraction, tree-sitter parsing, candidate links, diff classification, prompt conflicts, construction model: modules, declarations, import edges), goal checks read from any locked document (pass/violated/unknown with evidence, invariants outside the goal spec, `covers:` verification evidence), `internal/store` (input fingerprint, disposable current-state cache), `internal/serve` (loopback HTTP, `/api/state`, lifecycle, embedded assets), `web/` (TypeScript project map, goal map, rail, detail panels), `cmd/observe`.

## Hot invariants

- The product spec and system design are the source of truth for requirements and component boundaries.
- Read-only local operation. Never execute repository code, hooks, or build scripts from an analyzed project.
- Locked specs are the goal baseline. Task prompts and other docs are context only.
- The project map is construction, not requirement: modules come from tracked source, and an import becomes an edge only when it names a module or file here. Everything else stays external — a guessed dependency is a lie about the architecture.
- The map drills in place: project → module → file. Inside a module the edges are imports that stay within it, plus symbol use between its files (the only relation two files of one Go package have). A symbol-use edge is a name match inside one module, so it never crosses a package boundary and is never proof of a call.
- Analyzer evidence must carry provenance. Unknown stays unknown; code presence alone is not verification.
- Persistent state is only a replaceable current-analysis cache outside the repository. No history snapshots.
- All application access binds to loopback only; no LAN exposure.
- The V1 language set is Go and TypeScript. `internal/analyze/source.go` is the single place that decides it; every other language stays `unknown`.
- `nix build` uses `proxyVendor`, not `go mod vendor`: vendoring drops the tree-sitter C sources and headers that cgo compiles.
- Instance records live in the user cache directory (`observe/runs/<hash of repo root>.json`), never inside the analysed repository: the project stays read-only, and the record is disposable machine state.
- The UI is `web/` TypeScript with no framework and no remote asset: `web/build.sh` type-checks and bundles it to `internal/serve/static/app.js`, `nix build` rebuilds that bundle in its own derivation, and `assets_test.go` fails on drift. The page assigns no `innerHTML` and reaches no network; both are tested.

## Architecture elevator

```text
cmd/observe
    └── internal/serve  ──→  internal/repo
          │               ├→ internal/analyze ──→ internal/model
          ├────────────────→ internal/store  ───→ internal/model

web/ (TypeScript + SVG) ── built static assets embedded by internal/serve
```

- `cmd/observe/` — process entry point and startup.
- `internal/model/` — shared snapshot and status contract.
- `internal/repo/` — read-only Git and file access; system-design Repo reader.
- `internal/analyze/` — deterministic parsing and evidence-linked candidate relationships.
- `internal/store/` — replaceable SQLite current-state cache only; no historical snapshots.
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
| UI direction and tokens | `.agents/specs/design-system.md` (locked at `997f99f`) |
