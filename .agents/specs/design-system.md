<!-- design:locked:862d5b7 2026-10-31 -->

# Design system — Observe

- **Status:** Approved by the user and locked at `862d5b7` on 2026-10-31. The reviewed concept is a frozen input.
- **Upstream:** `.agents/specs/observe-progress-view.md`, `.agents/specs/system-design.md`, `.agents/specs/architecture.md` (all locked).

## Design goals and accepted references
- **Primary audience:** a developer directing AI coding work and checking whether changes align with project goals.
- **Product character:** calm engineering instrument; precise, quiet, evidence-first, candid about uncertainty. No gamified completion or unsupported green “done” state.
- **Primary view:** hierarchical goal-to-code map with a persistent whole-project overview, selected-diff highlight, and a detail panel.
- **Reference patterns:**
  - CodeSee Maps — collapsed hierarchical map and overview navigation: <https://docs.codesee.io/docs/explore-your-map>.
  - Sourcegraph — symbol hover and go-to-definition navigation: <https://sourcegraph.com/docs/code-navigation/features>.
  - These are interaction references only; do not copy brand, visual identity, or dense graph treatment.
- **Target:** macOS desktop browser, design frame 1440×900 CSS px; gracefully reflow down to 1100 px. Narrow/mobile layout is not a V1 target.

## Palette and contrast
- **Surfaces:** light default, neutral cool-gray canvas `#F3F5F8`, white panels `#FFFFFF`, subtle slate borders `#D8DEE8`; dark-mode tokens follow system preference.
- **Text:** primary `#17212F`, secondary `#526174`, muted `#697789`; minimum 4.5:1 contrast for normal text.
- **Accent:** restrained blue `#315FC5`; selected/focused state uses blue border plus a non-color focus ring.
- **Goal relationship states:**
  - Appears-to-support: blue outline `#315FC5` plus explicit label and solid marker.
  - Conflict: red `#B53C36` plus warning icon and `Conflict` label.
  - Unmapped: amber `#986300` plus dashed outline and `Unmapped` label.
  - Unknown/unavailable: slate `#687587` plus dotted outline and explicit label.
  - Stale/updating: violet `#6750A4` plus clock/progress label; stale results remain visibly stale.
  - Verification: a separate row/indicator, never inferred from relationship color; green `#237A52` is reserved for evidence-backed verified state and is accompanied by text/icon.
- **Accessibility:** every status uses text + shape/icon + color. Do not rely on color alone. Keyboard focus is visible. Respect `prefers-reduced-motion`; no essential information depends on animation.

## Typography
- **UI:** native system sans stack (`-apple-system`, BlinkMacSystemFont, Segoe UI, sans-serif); no remote fonts.
- **Code, paths, symbols, hashes:** system monospace stack.
- **Scale:** 12 px metadata, 13–14 px compact labels/body, 16 px section headings, 20–24 px project/goal headings. Body line height 1.45; code line height 1.5.
- **Hierarchy:** project → goal → module/file → source location. Status labels remain readable at 12 px minimum.

## Spacing, radius, elevation
- **Spacing scale:** 4, 8, 12, 16, 24, 32 px.
- **Map node padding:** 12–16 px; minimum 40 px interactive target where practical.
- **Corner radius:** 6 px controls, 8 px cards, 10 px detail panel; avoid pill-heavy decoration except compact status badges.
- **Elevation:** one subtle panel shadow only; map nodes rely on borders, grouping, and connectors rather than stacked shadows.

## Component inventory and states
- **App header:** repository identity, branch/commit, diff selector, refresh state; states: loading, repo unavailable, no diff selected, diff selected.
- **Goal rail:** all locked requirements with relationship status and count of linked code areas; selection syncs with map and detail panel; empty baseline state explicitly says goal alignment unavailable.
- **Project map:** SVG/canvas hierarchy `goals → areas/modules`; collapsed module groups preserve the whole-project shape. Connectors have provenance/uncertainty style. Selected diff highlights changed files/modules; unchanged code stays visible but subdued. Unsupported areas are shown as unknown, not omitted.
- **Detail panel:** selected goal/area, implementation relationship, separate verification status, evidence links to source/spec/test, provenance and inference label. Broken link shows unresolved state and does not retarget silently.
- **Diff overlay:** selected working-tree diff is visually distinct from relationship status (use a separate warm highlight/changed badge); toggling it never changes goal status.
- **Live status strip:** `Current`, `Updating`, `Stale`, `Failed`; show generation/time and retry. During edits, retain last successful result but label stale until latest fingerprint is analyzed.
- **Evidence row:** kind, path/symbol/line, provenance, confidence/uncertainty. Click opens exact source in detail pane while preserving map context.
- **No-spec state:** browsing remains available; goal alignment displays unavailable. Never derive approved goals from README or prompt.
- **Conflict state:** prompt/spec or locked-spec conflicts appear as unresolved warning; neither side silently wins.
- **Failure states:** stale, parser-unsupported, storage-unsaved, link-unresolved, and refresh-failed remain distinct and actionable.
- **Architecture section:** import cycles and hotspots as outline entries, with their counts in the section head; no cycle states that plainly rather than showing an empty list. Selecting a hotspot opens a panel with both factors and what made the declaration complex, and says the ranking compares declarations rather than totalling a file. The section is outline-only: architecture has no map of its own.
- **Data model section:** every declared table with its column and reference counts; a table a later statement changed in a way this reader does not model is marked, and the statement is named in its detail. A table reached by a route but declared by no migration is shown from the flow side, never invented here.
- **Flows section:** routes split into `Reaches a table` and `Stops before a table`, each route labelled with the step that stopped it. A stopped route keeps its place in the outline; stopping is a shown fact, not a hidden one.
- **Data map (ER):** tables as boxes with foreign keys as edges, laid out by reference depth so a table sits below those it references; a reference cycle settles at depth zero instead of looping. A reference to a table no migration declares draws no edge and claims no table. Selecting a table opens its detail with the migration that declared it, and the migration behind each reference.
- **Flow map:** routes in one column, the tables they reach in the other, with an edge only where the reader followed the whole chain. A route with no edge is counted below the map and remains in the outline, so a gap is visible rather than absent. Edge and box selection stay in step with the outline.
- **Map view switch:** `Project`, `Goals`, `Data`, `Flows`; the chosen view is marked, and switching never changes a verdict or a freshness label.

## Interaction rules
- **Overview → drill-in:** click goal/module to select; map remains visible and preserves context. Detail opens in a right-side panel with breadcrumb and return-to-overview path.
- **Whole-project visibility:** use collapsible levels and a mini-map/overview indicator, not a giant expanded graph. Selecting detail must not remove unrelated project areas from the overview.
- **Diff:** user selects the diff; changed files/modules receive a distinct overlay independent of status. Provide a toggle to show/hide diff highlighting.
- **Live update:** show updating after file changes; retain old analysis as stale; publish latest only after complete input fingerprint matches. Respect reduced motion; prefer subtle progress indicator over moving nodes.
- **Source navigation:** selecting evidence moves the detail panel to exact path/line; map and goal context remain in view.
- **Keyboard/accessibility:** tab through goals, map nodes, evidence, and actions; arrow keys may navigate siblings; Enter opens detail; Escape returns one level; all actions expose accessible names.

## Concept evidence
- **Reviewed:** interactive prototype at 1440×900 and 1100×800. At 1100 px the detail panel overlays the map while the goal rail and overall map remain visible; the initial overlap defect was corrected and reviewed.
- **Prototype:** `/var/folders/w1/yp3dpky17gsfy92yqsy457gm0000gn/T/observe-concept.2lFxJb/observe-concept.html` (temporary, sample data only; not part of the project).
- **Still pending:** user acceptance of the visual references, palette, typography, density, and interaction behavior.
- **Lock gate:** after explicit approval, create the production design-system artifact, add `design:locked` marker, and update AGENTS.md state.
