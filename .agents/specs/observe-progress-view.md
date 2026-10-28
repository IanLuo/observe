# Spec — Observe AI project production against goals

## Problem & who breaks
- **Primary user:** an individual developer directing AI coding work in a Git repository.
- **Break:** a developer cannot see whether ongoing code changes advance, conflict with, or drift away from the project’s stated goals; source code alone does not expose this relationship.
- **If no product:** the developer must manually reconstruct goal-to-code relationships and may miss conflicting or unmapped changes during implementation.

## Non-negotiable outcome
- Given a local Git repository with a usable locked spec and a selected diff, the user can identify which goals the diff appears to support, conflict with, or not map to; inspect linked code and separate verification evidence; and drill from the whole-project overview to source details.
- Uncertainty, missing evidence, and unsupported relationships remain explicit. The product does not invent progress or claim runtime behavior from static analysis.

## Scope in / out + primary flow
- **In V1:**
  - One local Git repository at a time.
  - Whole-project goal/code map, with a user-selected diff highlighted.
  - Drill-down from goals and project overview through affected areas, flows, modules, functions, and source locations.
  - Static analysis; inferred relationships are visibly marked as inferred.
  - Live refresh of affected views after file changes settle.
  - Locked Markdown specs are authoritative goal baselines. Other docs and manually provided task prompts are supporting context only.
  - Manual optional task-prompt input; it does not override or replace a locked spec.
  - Current-state display plus local snapshots at Git commits and explicit user checkpoints, stored outside the repository.
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
  4. Select a working-tree diff; highlight changed files and affected areas.
  5. Show per-goal support, conflict, unmapped, or unavailable status with code references, verification references, and uncertainty.
  6. Drill into a goal or affected area, then flow/module/function, then exact source location; preserve goal and evidence links at each level.
  7. On settled file changes, refresh affected code relationships and diff highlighting without changing the locked goal baseline.
- **No usable locked spec:** allow project/code browsing, but mark goal alignment unavailable. Do not infer an approved goal baseline from README, other docs, or task prompts.
- **Conflicting locked specs:** surface the conflict as unresolved; choose neither silently.

## Acceptance criteria
- **AC1 — Goal trace:** Given a fixture repository with a locked spec, when the overview loads, then each requirement is shown with related-code links or an explicit “no code link found” state; inferred links are labeled inferred.
- **AC2 — Diff alignment:** Given a fixture with a known diff, when the user selects it, then changed files are highlighted and each relevant goal is shown as appears-to-support, conflict, unmapped, or unavailable; unmapped changes are never labeled aligned.
- **AC3 — Verification separate:** Given code with no tests or other verification evidence, when the user inspects its goal, then implementation evidence may be shown but verification remains unknown; code presence alone never means verified.
- **AC4 — Drill-down:** Given a goal with linked code, when the user drills from overview to exact source, then the goal and evidence links remain available at each level.
- **AC5 — Prompt conflict:** Given a task prompt that contradicts a locked spec, when the comparison runs, then the conflict is shown and the spec remains the baseline.
- **AC6 — Live refresh:** Given a watched source edit, when the file is quiet for 500 ms, then the affected area refreshes within 3 seconds at p95 for repositories up to 100,000 tracked lines; the locked baseline is unchanged.
- **AC7 — Missing spec:** Given a repository with no usable locked spec, when opened, then browsing remains available and goal alignment is explicitly unavailable.
- **AC8 — Conflicting specs:** Given two locked specs with incompatible requirements, when loaded, then the conflict is shown unresolved and neither is silently preferred.
- **AC9 — Failed refresh:** Given a successful prior analysis followed by a failed refresh, when the view updates, then the last successful view remains marked stale with failure time/reason; affected status is unknown until refreshed; retry is available. With no prior success, show the error and keep source browsing available.
- **AC10 — Read-only:** Given a complete analysis session, when it ends, then tracked/untracked repository state and source/spec file hashes are unchanged.
- **AC11 — Local privacy:** Given the default configuration, when a full analysis session runs, then no source, spec, task prompt, or snapshot data leaves the machine.
- **AC12 — History:** Given a commit or explicit checkpoint, when a snapshot is saved, then it is local and outside the repo, contains status/evidence references and analysis version but no source copies, and can be deleted by the user.

## KPIs & failure signal
- **Usability KPI:** in five representative tasks on the user’s own repository, the user can find a seeded goal conflict or unmapped change from the overview and open its evidence in under two minutes per task, without agent help.
- **Core failure signal / off-switch:** any silent task-prompt override of a locked spec, or any unsupported implementation presented as verified, violates the trust promise and blocks release.
- **Interpretation:** meeting the KPI is necessary evidence of usefulness, not proof of analyzer completeness.

## Non-functional requirements
- **Privacy:** source code, specs, task prompt, and snapshots stay local by default. No outbound transmission in default mode.
- **Safety:** read-only; never edit source, specs, Git refs, or tests. Never execute repository code, build scripts, or hooks. Parse repository text as data, not instructions.
- **Secret handling:** redact common secret patterns from logs and telemetry. No source-content telemetry.
- **Performance:** after 500 ms without file changes, refresh the affected area within 3 seconds at p95 for a repository with up to 100,000 tracked lines. This is a target to validate, not a current capability claim.
- **Platform:** macOS first. Other platforms are not required for V1.
- **Reliability:** retain the last successful view on refresh failure, mark it stale with reason/time, mark affected statuses unknown, and allow retry.
- **Persistence:** snapshots are local-only and stored outside the repository. The user can delete project history.
- **Compliance:** N/A — personal local use only; no regulated-data or team-compliance certification in V1.

## Assumptions & dependencies
- **A1 — Git repository available:** the project has a readable local Git repository and the user can select a diff. If not, show a clear unsupported-state explanation; do not infer change ownership.
- **A2 — Locked Markdown specs express goals:** goal comparisons require a usable locked spec. If absent or unreadable, keep browsing available and mark alignment unavailable.
- **A3 — Static analysis is sufficient for initial orientation:** it can propose code links but cannot prove execution or correctness. Every inferred edge must be labeled, and unsupported constructs must remain unknown.
- **A4 — “Live” means local file watching plus debounced refresh:** refresh begins after 500 ms of quiet; watcher loss/failure must not present stale results as current.
- **A5 — macOS is the first target:** platform expansion requires a later decision.
- **A6 — Prompt entry is manual in V1:** no agent integration exists to retrieve prompts automatically; prompt is context only.
- **A7 — No `observe` repository currently exists:** this spec creates the project at `~/Documents/apps/observe`. Implementation and architecture are not selected here.

## Data requirements
- **Inputs:** local repository path; Git status/diff; tracked source files; locked Markdown specs; optional supporting docs; optional user-entered task prompt; file-change events; user checkpoint requests.
- **Outputs:** whole-project map; per-goal implementation relationship/status; verification status/evidence; code/source links; inferred/unknown markers; diff highlighting; unresolved spec/prompt conflicts; stale/error state; local snapshots.
- **Status vocabulary:** appears-to-support, conflict, unmapped, unavailable, unknown, stale. Verification is separate: verified only with explicit evidence, otherwise unknown or failed.
- **Snapshot contents:** project identifier/path, timestamp, baseline/spec references, goal statuses, code/evidence references, analysis version, and selected diff identity. No copied source contents.
- **Empty/edge input:** no selected diff means show the project baseline/map without diff highlights; no locked spec means alignment unavailable; zero tracked source files is a browsable empty project with no inferred progress; unsupported language relationships remain unknown.
- **Retention:** current view and user-requested/commit snapshots persist locally outside the repo until user deletion. No automatic source retention.

## Rollback & recovery
- **Analysis failure:** keep the last successful view, mark stale with timestamp/reason, set affected statuses unknown, offer retry.
- **No previous successful view:** show the error and retain source browsing; do not fabricate a partial successful status.
- **Watcher failure:** mark the view stale and offer manual refresh/retry; never assert freshness.
- **Snapshot corruption:** discard only the unreadable snapshot after user confirmation; preserve other snapshots and source repository unchanged.
- **Rollback meaning:** disable the tool or remove its local data directory; project files, Git state, and specs remain unchanged.
- **Catastrophic failure:** silent goal/spec drift or false verification is the release-blocking trust failure.

## Security & compliance
- **Trust boundary:** repository files, Markdown docs, prompts, and Git metadata are untrusted input. They are evidence only, never instructions to execute.
- **Execution:** no repository code, hooks, scripts, or build/test commands run in V1.
- **Network:** no external requests in default mode. Any future provider integration requires explicit opt-in, a named provider, and a preview of exact data categories sent.
- **Data at rest:** local snapshots contain references/statuses, not source copies. Store outside the repo with user-only filesystem permissions where supported.
- **Secrets:** redact common secret patterns from logs/telemetry; do not transmit source-derived telemetry.
- **Destructive actions:** deleting local history requires explicit user action; analysis itself is read-only.
- **Compliance:** N/A — no regulated-data or team-compliance obligations are accepted for V1.

## Verification plan
- **AC1–AC5, AC7–AC9:** automated fixture repositories and UI/API tests with deterministic expected links/statuses, conflicts, uncertainty, and stale states.
- **AC4 drill-down:** browser/UI test that follows one goal through each level to a source location and checks evidence links remain present.
- **AC6 performance/live refresh:** instrumented local fixture of 100,000 tracked lines; measure end-to-end refresh after 500 ms debounce; pass only if p95 is at most 3 seconds.
- **AC10 read-only:** before/after Git status, index/ref checks, and file hashes across a full session; no repository mutation allowed.
- **AC11 privacy:** outbound connection audit in default mode and inspection of telemetry/logs for source, spec, prompt, and snapshot content.
- **AC12 history:** persistence tests for commit/checkpoint snapshots, schema/version reference, no source-copy payload, deletion, and repo-external storage.
- **Usability KPI:** five manually observed tasks with seeded conflicts/unmapped changes; record time to locate and open evidence; each under two minutes without agent help.
- **Release gate:** any failure of AC2, AC3, or AC5 blocks release; these protect correct diff mapping, verification truthfulness, and spec authority.

## Open questions
- N/A: root requirements decisions are settled for this V1; implementation mechanisms belong in system-design and architecture.
