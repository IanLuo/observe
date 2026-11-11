<!-- specs:locked:73f2eeb 2026-11-11 type=system-design -->

## Link contract
- **upstream** (this doc relies on): .agents/specs/observe-progress-view.md
- **referrers** (must cite this when they change): .agents/specs/architecture.md

# System design — Observe
## Components & ownership
- **Product owner:** the local Observe application, initially maintained by one developer. All components are in-process boundaries in V1; no service or worker process.
- **Repo reader:** reads repository identity, Git commit/status/diff, locked Markdown specs, supporting docs, and tracked source. Refuses source/spec/Git writes, code execution, hooks, and build/test execution.
- **Refresh coordinator:** tracks input generations, schedules analysis, and requests full reconciliation when impact is unknown or inputs change broadly. Refuses to publish a result whose input fingerprint is no longer current. In V1 a refresh is requested — the Refresh action, or the page regaining attention — rather than watched for: the watcher path below is designed and optional.
- **Analyzer:** extracts locked-spec requirements, code structure, references, tests, schema entities and relations from SQL migrations, request routes and the store tables they reach (routes read from literal registrations; a hop the reader cannot follow stops with its reason, never a guessed table), and candidate links using deterministic local parsing and reference matching. Every link includes provenance. Refuses to claim runtime truth, verified behavior from code presence alone, or a relationship unsupported by evidence.
- **Evidence store:** stores one disposable current-analysis cache outside the repository. Refuses history snapshots, source copies, prompt copies, and project-file changes.
- **Presenter:** displays the full project map, selected-diff highlighting, goal/code drill-down, evidence, uncertainty, errors, and freshness. Refuses to convert inferred/unknown/stale states into current or verified claims.
- **Boundary ownership:** each component owns only its named job. Shared state crosses component boundaries through the versioned immutable `AnalysisSnapshot` contract below; persistence contains only the replaceable current cache.

## Data model
- **Authority:** Git, source files, and locked specs are the source of truth. Analysis results are derived and rebuildable. Other docs and manually entered task prompts are context only; prompt content is not persisted.
- **Repository identity:** canonical local root path plus repository identity (Git common-dir identity where available). A moved or replaced repository is a different identity until re-opened and validated.
- **Goal locator:** `spec path + heading chain + normalized requirement-text hash`. The normalized requirement text may be carried as a display label; the full spec is not copied into the cache. If text or location changes, mark the old link changed/unresolved and require explicit confirmation before treating it as the same goal. Never edit locked specs to insert IDs.
- **Schema entity and relation:** a table (name, columns, keys, declaring migration path) and a foreign key (from table/columns, to table/columns, declaring migration path). Derived from applied migrations in file-name order; never from a database.
- **Evidence reference:** repository-relative path, symbol/name when available, line range when available, evidence kind (declaration/reference/test/spec/other), provenance (exact match/parser rule/manual context), confidence/uncertainty, and optional verifier/test result reference. A reference is not proof of runtime behavior.
- **Goal assessment:** goal locator; implementation relation (`appears-to-support`, `conflict`, `unmapped`, `unavailable`, `unknown`, or `stale`); evidence references; inference marker; diagnostics. Verification is separate (`verified` only with explicit evidence, otherwise `unknown` or `failed`).
- **Input fingerprint:** repository identity; current commit; selected-diff hash (hash only, never diff contents); locked-spec paths and content hashes; relevant source-file hashes; analyzer version; cache schema version. The optional task prompt is transient context and excluded from persistence.
- **`AnalysisSnapshot` (immutable in-memory exchange record):** input fingerprint; generation ID; created time; freshness (`current`, `stale`, `failed`); goal assessments; evidence references; verification records; diagnostics; analyzer/schema version. Persistent cache stores only the latest valid record; no historical snapshots. Stores derived statuses and references only, never source copies or prompt copies.
- **Current cache:** at most one replaceable latest analysis for the open repository. Valid only when its complete input fingerprint and analyzer/schema version match current inputs. Invalid cache is discarded and recomputed; there is no persisted history.
- **Stale reads:** a changed commit, diff hash, spec/source hash, repository identity, or analyzer/schema version makes the cache stale. During refresh, keep the prior result in memory and visibly stale; after a successful refresh replace it. On restart, discard invalid cache and recompute; do not restore historical results.
- **Empty/missing fields:** absent optional prompt is valid; no selected diff means baseline-only view; no usable locked spec means goal alignment unavailable; no evidence means an explicit empty evidence set and unmapped/unknown status; unreadable source/spec means diagnostic + unknown/unavailable, never empty-success.

## Boundaries & interfaces
- **Repo reader → Refresh coordinator:** file/Git/spec change events with repository-relative paths and observed revision/hash. Events are hints; the coordinator reconciles against current inputs so missed events cannot silently preserve freshness.
- **Refresh coordinator → Repo reader:** read request for a consistent input generation: repo identity, selected diff, relevant file/spec paths, and expected hashes. Reader returns content-derived parse inputs and hashes, or a typed read error. Reader never returns executable instructions for execution.
- **Repo reader → Analyzer:** immutable in-memory text/metadata for the requested generation; no source copy is written to persistent storage. Parsing treats all text as untrusted data.
- **Analyzer → Evidence store:** a complete `AnalysisSnapshot` candidate plus its input fingerprint. Store accepts it as current only if the coordinator confirms the fingerprint still matches; otherwise it is stale/discarded. No history writes or checkpoint writes occur.
- **Evidence store → Presenter:** current cache with freshness state. Presenter must not hide diagnostics or reinterpret statuses.
- **Refresh coordinator → Presenter:** lifecycle events (`updating`, `current`, `stale`, `failed`) with generation and timestamp. Older generations cannot replace newer ones.
- **Presenter → Refresh coordinator / Evidence store:** user actions to select a diff, retry, manually refresh, or clear the current cache. No checkpoint/history action and no mutation of repo files or Git refs is exposed.
- **Cache compatibility:** the persisted current cache has schema and analyzer versions. Unknown future schema is not guessed or partially loaded; clear only the disposable cache, show an incompatibility diagnostic, and recompute from source while permitting browsing.
- **Consistency rule:** each published snapshot describes one coherent fingerprint. If a read sees files from mixed revisions, cancel and retry; never publish partial analysis as current.

## Failure modes
- **Repo reader unavailable/unreadable input:** report the failing path/operation; do not substitute empty success. If the repo changes during read, cancel that generation and retry using a new fingerprint.
- **Malformed or unsupported parser input:** keep unaffected findings; mark affected goals/links unknown with diagnostics. A spec parse failure makes goal alignment unavailable. Do not treat parser failure as proof of no implementation.
- **Analyzer slow or fails:** keep prior result in memory visibly stale during the session; affected statuses become unknown; show failure time/reason and retry. With no prior success, show error and retain source browsing. Do not preserve failed or stale results as history on disk.
- **Evidence store cache read/write failure:** continue browsing and any unaffected live analysis where possible; mark results unsaved/stale. Never write a fallback inside the repository.
- **Current cache corrupt:** discard the disposable cache, report the issue, recompute from source/specs, and preserve the repository unchanged. No history records exist to delete.
- **Broken source/evidence link:** show the unresolved reference and unknown state; do not silently retarget to a nearby symbol or line.
- **Request flow stopped at an unbound dependency:** a handler that calls a use case or store the composition root supplies, through a binding this reader does not follow, stops with that reason and shows no table. Binding injected use cases is a new rule rather than a fix, so the route stays unknown until that rule is decided; the stop names the call, so a reader can see what was not followed.
- **Unknown dependency impact:** immediately mark current analysis stale; run full reconciliation. If reconciliation fails, retain prior results as stale and offer retry/manual refresh.
- **Presenter failure:** analysis data remains local and unchanged; on reload, validate the cache fingerprint and restore only if current, otherwise discard and recompute.
- **Availability policy:** fail open for source browsing and unaffected analysis; fail closed for claims about affected goals. Keep stale data visible but label it; never label it current/verified until supported.
- **Release-blocking failure:** any stale or inferred result presented as current, any silent locked-spec override, or implementation shown as verified without evidence. These violate the product trust contract.

## Scale assumptions
- **Users:** one local user and one open repository per application instance in V1; no shared server, team concurrency, or multi-repo dashboard.
- **Repository size:** up to 100,000 tracked lines, matching the locked product-spec performance target. Files not parseable by the local analyzer remain visible but their relationships are unknown.
- **Change rate:** bursty local writes are coalesced after 500 ms quiet. At most one analysis generation is authoritative; latest consistent input wins, and superseded work is cancelled or discarded.
- **Latency:** affected-area refresh target is p95 ≤3 seconds after the quiet period at the supported repository size. If exceeded, show progress and preserve the last successful view as stale.
- **Storage:** only one current cache is retained per open repository identity; replace it after successful analysis and remove it when cleared. No historical snapshots or checkpoint storage. Cache size is bounded by the current analysis output; no user-controlled retention policy is needed.
- **Ten-times behavior:** above the supported repository size or when dependency impact cannot be bounded, do not claim the latency target; surface the limit, use full reconciliation where feasible, and mark unprocessed relationships unknown.
