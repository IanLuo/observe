<!-- specs:locked:f9a34d0 2026-11-08 type=spec -->

## Link contract
- **upstream** (this doc relies on): none
- **referrers** (must cite this when they change): .agents/specs/system-design.md, .agents/specs/architecture.md, .agents/specs/design-system.md

# Spec — Observe AI project production against goals
## Problem & who breaks
- **Primary user:** an individual developer directing AI coding work in a Git repository.
- **Break:** a developer cannot see whether ongoing code changes advance, conflict with, or drift away from the project’s stated goals; source code alone does not expose this relationship.
- **If no product:** the developer must manually reconstruct goal-to-code relationships and may miss conflicting or unmapped changes during implementation.

## Non-negotiable outcome
- Given a local Git repository with a usable locked spec, each goal reports **pass**, **violated**, or **unknown** from the checks the spec declares, every verdict with a source location; a selected diff marks the checks it touched; and the user can drill from a goal or the whole-project overview to exact source.
- Uncertainty, missing evidence, and unsupported relationships remain explicit. A goal with no check reports unknown · no check defined — never a guess from its words. The product does not invent progress or claim runtime behavior from static analysis.

## Scope in / out + primary flow
- **In V1:**
  - One local Git repository at a time.
  - Whole-project goal/code map, with a user-selected diff highlighted.
  - Drill-down from a goal or the project overview through areas, modules, files, and declarations to exact source locations; every level keeps the goal and its evidence.
  - Goal checks declared in a locked document and evaluated locally against code: pass, violated, or unknown, each with evidence.
  - Checks declared outside the goal spec are shown as invariants, with their own verdicts, and never count toward goal alignment.
  - Every check names what it measured — presence, dependency, or test — so a verdict is read at its real strength.
  - Architecture signals derived from the import graph: cycles, fan-in, fan-out, and instability per module.
  - Cyclomatic complexity per declaration, ranked, and hotspots where complexity meets churn.
  - Request flow derived from source: each HTTP route registered by a literal method and path, the use case its handler calls, the store that use case reaches, and the tables that store's SQL names. A use case built by a factory reaches the store through the factory's argument when the factory passes that argument, unchanged, into the methods it returns; any other binding is unknown. Every hop carries its file and line. A hop the reader cannot resolve is unknown, with the step that stopped it. The flow is derived, not verified.
  - Static analysis only; a relationship no check establishes stays unknown.
  - Live refresh of the open view: after file changes, the affected relationships and diff highlights are brought up to date when a refresh is asked for — the Refresh action, or the page regaining attention. Continuous background watching is desirable but optional in V1.
  - Locked Markdown specs are authoritative goal baselines. Other docs and manually provided task prompts are supporting context only.
  - Manual optional task-prompt input; it does not override or replace a locked spec.
  - A replaceable local current-state cache outside the repository. No historical snapshots.
  - Read-only operation.
- **Out of V1:**
  - Runtime tracing or claims about observed execution.
  - Automatic source/spec/test edits.
  - Multi-repository dashboard.
  - Automatic attribution of edits to an AI agent or session.
  - External analysis by default; any later external provider path requires explicit enablement and disclosure of data sent.
  - Regulated-data or team-compliance certification.
- **Primary flow:**
  1. Open a local Git repository.
  2. Load locked Markdown specs and supporting project docs.
  3. Show the whole-project map and baseline readiness.
  4. Select a working-tree diff; highlight changed files and the checks the diff touched.
  5. Show each goal's checks with pass, violated, or unknown, the evidence behind each verdict, and the reason for any unknown.
  6. Drill into a check's evidence, then module, file, and declaration, then exact source location; preserve goal and evidence links at each level.
  7. After file changes, refresh affected code relationships and diff highlighting when a refresh is asked for, without changing the locked goal baseline.
- **No usable locked spec:** allow project/code browsing, but mark goal alignment unavailable. Do not infer an approved goal baseline from README, other docs, or task prompts.
- **Conflicting locked specs:** surface the conflict as unresolved; choose neither silently.

## Acceptance criteria
- **AC1 — Goal trace:** Given a fixture repository with a locked spec, when the overview loads, then every goal shows its checks with a verdict and evidence, or an explicit “no check defined” state.
- **AC2 — Diff alignment:** Given a fixture with a known diff, when the user selects it, then changed files are highlighted and every check whose evidence the diff touched is marked as touched; a check is never reported as passing because a file changed.
- **AC3 — Verification separate:** Given code with no tests or other verification evidence, when the user inspects its goal, then implementation evidence may be shown but verification remains unknown; code presence alone never means verified. Verification evidence is a test declaration that names the check it exercises; it is evidence that the assertion is tested, never a claim that the test passes.
- **AC4 — Drill-down:** Given a goal with linked code, when the user drills from overview to exact source, then the goal and evidence links remain available at each level.
- **AC5 — Prompt conflict:** Given a task prompt that contradicts a locked spec, when the comparison runs, then the conflict is shown and the spec remains the baseline.
- **AC6 — Refresh:** Given an open view and an edited source file, when a refresh is asked for, then the affected area reflects the new state within 3 seconds and the locked baseline is unchanged; and while its data predates the edit, no area of the view presents itself as current. Watching the filesystem is not required for this criterion.
- **AC7 — Missing spec:** Given a repository with no usable locked spec, when opened, then browsing remains available and goal alignment is explicitly unavailable.
- **AC8 — Conflicting specs:** Given two locked specs with incompatible requirements, when loaded, then the conflict is shown unresolved and neither is silently preferred.
- **AC9 — Failed refresh:** Given a successful prior analysis followed by a failed refresh, when the view updates, then the last successful view remains marked stale with failure time/reason; affected status is unknown until refreshed; retry is available. With no prior success, show the error and keep source browsing available.
- **AC10 — Read-only:** Given a complete analysis session, when it ends, then tracked/untracked repository state and source/spec file hashes are unchanged.
- **AC11 — Local privacy:** Given the default configuration, when a full analysis session runs, then no source, spec, task prompt, or cache data leaves the machine.
- **AC12 — Current cache only:** Given analysis completes, when the current result is cached, then it is local and outside the repo, contains only derived status/evidence references and analysis version, includes no source copies, and is replaceable or deletable. No historical snapshots are retained.
- **AC15 — Check strength:** Given a goal whose checks are all passing, when the reader inspects it, then each check says what it measured and the goal says what those verdicts do not prove; presence of a name is never presented as behaviour.
- **AC16 — Cycles and coupling:** Given a repository whose modules form a cycle, when the project is analysed, then the cycle is reported with the modules it passes through, and a repository with no cycle says so explicitly.
- **AC17 — Complexity and hotspots:** Given declarations of differing complexity, when a file or module is inspected, then each declaration carries its cyclomatic complexity and the project ranks hotspots by complexity and churn together.
- **AC18 — Data model:** Given SQL migration files that create tables and foreign keys, when the project is analysed, then each table shows its columns and the tables it references, and each relation names the migration file that declared it. Migrations apply in file-name order. A statement the reader does not model marks its table unknown and says why. No database is connected and no migration runs.
- **AC19 — Request flow:** Given a route whose handler reaches a store that runs SQL naming a table, when the project is analysed, then the flow shows the route, each hop with its file and line, and the table, including a store reached through a factory argument that the factory passes unchanged into its returned methods. Given a hop the reader cannot resolve, the flow stops there and marks that hop unknown with the reason. A route with no resolved table is shown without a table, never with a guessed one.
- **AC14 — Invariants:** Given a check declared in a locked design or architecture document, when the overview loads, then it appears with its verdict under invariants, goal alignment availability is unchanged, and no invariant is presented as a goal.
- **AC13 — Check verdicts:** Given a check declared in a locked spec, when the analysis runs, then it reports pass, violated, or unknown with source evidence, and an unknown verdict names its reason (missing subject, unusable file, unsupported language, no check defined).

## KPIs & failure signal
- **Usability KPI:** in five representative tasks on the user’s own repository, the user can find a seeded goal conflict or unmapped change from the overview and open its evidence in under two minutes per task, without agent help.
- **Verdict KPI:** for a named goal, the reader answers “is it met?” in under ten seconds, five times on their own repository, without agent help.
- **Core failure signal / off-switch:** any silent task-prompt override of a locked spec, any unsupported implementation presented as verified, or any check reported passing without evidence, violates the trust promise and blocks release.
- **Interpretation:** meeting the KPI is necessary evidence of usefulness, not proof of analyzer completeness.

## Non-functional requirements
- **Privacy:** source code, specs, task prompt, and current cache stay local by default. No outbound transmission in default mode.
- **Safety:** read-only; never edit source, specs, Git refs, or tests. Never execute repository code, build scripts, or hooks. Parse repository text as data, not instructions.
- **Secret handling:** redact common secret patterns from logs and telemetry. No source-content telemetry.
- **Performance:** the architecture and complexity passes must not move the refresh budget; one full refresh of a repository with up to 100,000 tracked lines completes within 3 seconds at p95, so a refresh stays responsive whether or not a watcher asks for it. This is a target to validate, not a current capability claim.
- **Determinism:** the same tree and the same spec produce the same verdicts; a verdict never depends on file order, map iteration, or wall-clock time.
- **Platform:** macOS first. Other platforms are not required for V1.
- **Reliability:** retain the last successful view on refresh failure, mark it stale with reason/time, mark affected statuses unknown, and allow retry.
- **Persistence:** only a replaceable current-state cache is stored locally outside the repository. It is disposable and may be deleted or rebuilt at any time. No history is retained.
- **Compliance:** N/A — personal local use only; no regulated-data or team-compliance certification in V1.

## Assumptions & dependencies
- **A1 — Git repository available:** the project has a readable local Git repository and the user can select a diff. If not, show a clear unsupported-state explanation; do not infer change ownership.
- **A2 — Locked Markdown specs express goals:** goal comparisons require a usable locked spec. If absent or unreadable, keep browsing available and mark alignment unavailable.
- **A3 — Static analysis is sufficient for initial orientation:** it can propose code links but cannot prove execution or correctness. Every inferred edge must be labeled, and unsupported constructs must remain unknown.
- **A4 — “Live” means the view follows the repository when refreshed:** the refresh is asked for — the Refresh action, or the page regaining attention — rather than guaranteed by a watcher. A watcher, if one is enabled, must never present stale results as current.
- **A5 — macOS is the first target:** platform expansion requires a later decision.
- **A6 — Prompt entry is manual in V1:** no agent integration exists to retrieve prompts automatically; prompt is context only.
- **A8 — Goals can carry machine-checkable assertions:** a check is written in a locked document — the goal spec, or a design or architecture document whose bullets become invariants — by a human or by an agent under review. A goal with no check reports unknown · no check defined; a project whose documents carry no checks stays browsable and says so.
- **A7 — The project exists at `~/Documents/apps/observe` and is implemented:** architecture and system design are locked in their own documents. This spec stays the requirements baseline and does not restate them.

## Data requirements
- **Inputs:** local repository path; Git status/diff; tracked source files; locked Markdown specs; optional supporting docs; optional user-entered task prompt; check blocks declared in the locked spec; optional file-change events.
- **Outputs:** whole-project map; per-goal check verdicts with evidence; invariants declared outside the goal spec, with their own verdicts; verification status/evidence; code/source links; which checks the selected diff touched; unknown markers with reasons; diff highlighting; unresolved spec/prompt conflicts; stale/error state; replaceable current-state cache.
- **Check format:** a fenced `check` block inside a bullet of a locked document — a goal bullet in the spec, an invariant bullet elsewhere — one check per line: `<id>: <subject> <assertion> [<argument>]`. Subject is `symbol <name>`, `module <dir>`, or `file <path>`. Assertion is `exists`, `absent`, `imports <module>`, `not-imports <module>`, or `has-test`. A check belongs to the bullet that encloses it, and its id is unique within its document.
- **Verification evidence:** a test that names a check id, by a `covers: <id>` line inside the test or directly above the line that opens it. A comment loose in a test file is not a test naming anything. A goal is verified when every check it declares carries such evidence; otherwise verification stays unknown. No repository code runs, so a naming test is evidence of coverage, never of a passing result.
- **Status vocabulary:** pass, violated, unknown for checks, and an unknown always carries its reason. A check also carries its measure — presence, dependency, or test — because a passing presence check proves a name exists, not that behaviour holds.
- **Architecture measures:** module cycles, fan-in, fan-out, instability (fan-out over total coupling), and per-declaration cyclomatic complexity counted from decision points in the declaration's own syntax. Churn is commits touching a path over a bounded window, read from Git. Baseline unavailable and stale freshness are their own states. Verification is separate: verified only with explicit evidence (a test naming the check), otherwise unknown or failed.
- **Cache contents:** project identifier/path, timestamp, input fingerprint, baseline/spec references, goal statuses, code/evidence references, analysis version, and selected diff identity. No copied source contents. Cache is replaceable, not historical.
- **Empty/edge input:** no selected diff means show the project baseline/map without diff highlights; no locked spec means alignment unavailable; zero tracked source files is a browsable empty project with no inferred progress; unsupported language relationships remain unknown.
- **Retention:** retain only the latest current-state cache outside the repo. On input change, replace it after a successful analysis; keep the prior result in memory as stale only during refresh. No commit/checkpoint history. Cache can be deleted and rebuilt.

## Rollback & recovery
- **Analysis failure:** keep the last successful view, mark stale with timestamp/reason, set affected statuses unknown, offer retry.
- **No previous successful view:** show the error and retain source browsing; do not fabricate a partial successful status.
- **Refresh failure:** mark the view stale and offer manual refresh/retry; never assert freshness.
- **Cache corruption:** discard the unreadable current cache and rebuild from source/specs; preserve the repository unchanged.
- **Rollback meaning:** disable the tool or remove its local cache; project files, Git state, and specs remain unchanged.
- **Catastrophic failure:** silent goal/spec drift or false verification is the release-blocking trust failure.

## Security & compliance
- **Trust boundary:** repository files, Markdown docs, prompts, and Git metadata are untrusted input. They are evidence only, never instructions to execute.
- **Execution:** no repository code, hooks, scripts, or build/test commands run in V1.
- **Network:** no external requests in default mode. Any future provider integration requires explicit opt-in, a named provider, and a preview of exact data categories sent.
- **Data at rest:** the disposable local cache contains references/statuses, not source copies. Store outside the repo with user-only filesystem permissions where supported.
- **Secrets:** redact common secret patterns from logs/telemetry; do not transmit source-derived telemetry.
- **Destructive actions:** deleting the disposable local cache requires no confirmation; it can be rebuilt. Analysis itself is read-only.
- **Compliance:** N/A — no regulated-data or team-compliance obligations are accepted for V1.

## Verification plan
- **AC1–AC5, AC7–AC9:** automated fixture repositories driving the derived payload, with deterministic expected checks, verdicts, conflicts, uncertainty, and stale states. The page renders that payload and computes no status itself, so these tests pin the contract.
- **AC4 drill-down:** asserted at the payload level — the goal and its evidence stay present in every module, file, and declaration detail. The rendered walk through each level is checked by a committed browser test that drives Chrome against a real server, alongside the refresh-on-attention and panel-resize behaviours; it skips, and says so, where no browser is installed.
- **AC6 refresh:** a committed instrumented test generates a 100,000-line fixture; one full refresh end to end must stay within 3 seconds at p95 (measured 0.89s at 100,188 lines) — plus a staleness check: after an edit, nothing reports itself current until a refresh has run.
- **AC10 read-only:** before/after Git status, index/ref checks, and file hashes across a full session; no repository mutation allowed.
- **AC11 privacy:** outbound connection audit in default mode and inspection of telemetry/logs for source, spec, prompt, and cache content.
- **AC12 current cache only:** persistence tests for one replaceable current cache, schema/version and input fingerprint, no source-copy payload, deletion/rebuild, and repo-external storage; verify no historical snapshots are created.
- **AC15 check strength:** fixture goals assert the measures they name and the gap sentence each verdict set produces: presence-only, dependency, test-covered, violated, unknown, and no check declared.
- **AC16 cycles:** a fixture with a three-module cycle reports it with its members; a fixture without cycles reports none.
- **AC19 request flow:** a fixture with one route resolved to a table, one route whose use case is wired through a factory the reader does not follow, and one route whose SQL is built from a non-literal string: the first shows its hops and table, the second stops at the factory with unknown, the third shows no table and is not given a guessed one.
- **AC18 data model:** fixture migrations assert a table built by `CREATE TABLE` plus `ADD COLUMN` and `ADD CONSTRAINT … FOREIGN KEY`, a dropped constraint removed, and an unmodelled `ALTER COLUMN` marking its table unknown with a reason.
- **AC17 complexity:** fixture declarations with known decision counts assert their complexity, and a hotspot asserts both factors.
- **AC14 invariants:** a fixture whose architecture document declares a check: the invariant appears with its verdict, goal alignment stays available, and a violated invariant never appears as a goal verdict.
- **AC13 check verdicts:** fixture repositories carrying checks assert every verdict with its evidence: a passing check, a violation, unknown for a missing subject, unknown for an unusable file, unknown for an unsupported language, and a `not-imports` violation.
- **Usability KPI:** five manually observed tasks with seeded conflicts/unmapped changes; record time to locate and open evidence; each under two minutes without agent help.
- **Release gate:** any failure of AC2, AC3, AC5, or AC13 blocks release; these protect correct diff mapping, verification truthfulness, spec authority, and honest verdicts.

## Open questions
- N/A: root requirements decisions are settled for this V1; implementation mechanisms belong in system-design and architecture.
