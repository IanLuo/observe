// Header, baseline, the per-file diff, detail panels, conflicts, and footer.
// Each function renders server-decided values and nothing else: a badge always
// carries the server's word for the state (architecture D5).

import {
  badge,
  checkStyle,
  clear,
  el,
  locationText,
  verificationStyle,
  type StatusStyle,
} from "./dom";
import { checksOf, dirOf, evidenceOf, goalAreas, goalStatus } from "./map";
import { DiffFile, Goal, Ref, State } from "./types";

export interface PanelHandlers {
  onSelect: (key: string | null) => void;
  onRefresh: () => void;
  onClearCache: () => void;
  onToggleDiff: () => void;
}

const freshnessStyle: Record<string, StatusStyle> = {
  current: { icon: "●", word: "Current", cls: "st-current" },
  updating: { icon: "⏱", word: "Updating", cls: "st-stale" },
  stale: { icon: "⏱", word: "Stale", cls: "st-stale" },
  failed: { icon: "⚠", word: "Failed", cls: "st-conflict" },
};

export function renderHeader(root: HTMLElement, state: State, showDiff: boolean, h: PanelHandlers): void {
  root.replaceChildren();

  const name = state.repository.split("/").filter(Boolean).pop() ?? state.repository;
  const identity = el("div", { class: "identity" }, [
    el("h1", { text: name, title: state.repository }),
    el("div", { class: "meta", text: [state.commit ? state.commit.slice(0, 12) : "", state.generation ? "gen " + state.generation : ""].filter(Boolean).join(" · ") }),
  ]);

  const style = freshnessStyle[state.freshness] ?? { icon: "?", word: state.freshness, cls: "st-unknown" };
  const strip = el("div", { class: "strip" }, [badge(style)]);
  if (state.updated_at) strip.append(el("span", { class: "meta", text: "analysed " + state.updated_at.replace("T", " ").replace("Z", " UTC") }));
  if (state.failure) {
    strip.append(
      el("span", {
        class: "meta warn",
        text: `${state.failure.at.replace("T", " ").replace("Z", " UTC")}: ${state.failure.reason}`,
        title: state.failure.reason,
      }),
    );
  }

  const diff = button("Show diff", "Toggle the selected-diff highlight", h.onToggleDiff);
  // The pressed state is the toggle's own state, not whether a diff exists: a
  // button that says "pressed" while the highlight is hidden lies to a screen
  // reader.
  diff.setAttribute("aria-pressed", showDiff ? "true" : "false");
  const actions = el("div", { class: "actions" }, [
    button("Refresh", "Retry the analysis now", h.onRefresh),
    diff,
    button("Clear cache", "Discard the disposable analysis cache", h.onClearCache),
  ]);

  root.append(identity, strip, actions);
}

function button(label: string, title: string, onClick: () => void): HTMLButtonElement {
  const b = el("button", { text: label, title });
  b.type = "button";
  b.addEventListener("click", onClick);
  return b;
}

export function renderBaseline(root: HTMLElement, state: State): void {
  const specs = state.baseline.specs ?? [];
  if (state.baseline.state === "available") {
    root.className = "baseline";
    root.textContent = "Baseline: " + specs.join(", ");
    return;
  }
  root.className = "baseline unavailable";
  root.textContent =
    state.baseline.state === "conflicting"
      ? `Goal alignment unresolved: ${specs.join(" and ")} both claim the baseline. Neither is preferred.`
      : "Goal alignment unavailable: no usable locked spec. Browsing stays available.";
}

/**
 * renderFileDiff shows the changed lines of one file, in the detail panel where the
 * file itself is shown. The diff belongs to the file: a separate list of every
 * change in the repository repeats what the reader has already navigated past.
 */
export function renderFileDiff(root: HTMLElement, state: State, path: string, showDiff: boolean): void {
  const file = (state.diff.files ?? []).find((f) => f.path === path);
  if (!file) return;

  root.append(
    el("h3", { text: "Selected diff" }),
    el("p", { class: "meta", text: `${file.status} · +${file.added} −${file.removed}` }),
  );
  if (!showDiff) {
    root.append(el("p", { class: "empty", text: "Diff lines hidden. The status is unchanged by this toggle." }));
    return;
  }
  if (!file.lines || file.lines.length === 0) {
    root.append(el("p", { class: "empty", text: "No line changes to show." }));
    return;
  }
  const lines = el("div", { class: "lines" });
  for (const line of file.lines) {
    lines.append(
      el("div", { class: line.kind === "added" ? "add" : "del" }, [
        el("span", { class: "num", text: String(line.line) }),
        el("span", { class: "text", text: (line.kind === "added" ? "+ " : "− ") + line.text }),
      ]),
    );
  }
  if (file.truncated) lines.append(el("div", { class: "empty", text: `… ${file.truncated} more changed lines` }));
  root.append(lines);
}

export function renderDetail(root: HTMLElement, state: State, selected: string | null, showDiff = true): void {
  clear(root);
  const goals = state.goals ?? [];

  if (!selected) {
    root.append(el("h2", { text: "Detail" }));
    root.append(el("p", { class: "empty", text: "Select a module, a goal, an area, or a changed file." }));
    root.append(overview(state));
    return;
  }

  if (selected.startsWith("f:")) {
    const path = selected.slice(2);
    const file = (state.diff.files ?? []).find((f) => f.path === path);
    root.append(el("h2", { text: "Changed file" }));
    root.append(el("div", { class: "crumbs", text: dirOf(path) + " › " }));
    root.append(el("div", { class: "path big", text: path }));
    if (file) root.append(diffFacts(file));
    renderFileDiff(root, state, path, showDiff);
    root.append(linkedGoals(state, path));
    return;
  }

  if (selected.startsWith("a:")) {
    renderArea(root, state, selected.slice(2));
    return;
  }

  const goal = goals[Number(selected.split(":")[1])];
  if (!goal) {
    root.append(el("p", { class: "empty", text: "That selection is no longer in the analysis." }));
    return;
  }

  root.append(el("div", { class: "crumbs", text: [goal.spec_path, ...goal.heading].join(" › ") }));
  root.append(el("h2", { text: goal.label }));
  root.append(el("div", { class: "badges" }, [badge(checkStyle(goalStatus(goal)))]));

  root.append(el("p", { class: "meta", text: "Goal read from the locked spec; the verdicts come from the checks it declares." }));

  const checks = checksOf(goal);
  root.append(el("h2", { class: "sub", text: "Checks" }));
  if (checks.length === 0) {
    root.append(el("p", { class: "empty", text: "No check defined for this goal. Nothing is inferred from its words." }));
  } else {
    const list = el("ul", { class: "checks" });
    for (const check of checks) {
      const facts = [`${check.subject} ${check.assertion}`, `declared ${locationText(check.spec_path, check.line, check.line)}`];
      if (check.touched) facts.push("touched by the selected diff");
      const row = el("li", { class: "check" }, [
        el("div", { class: "check-head" }, [
          badge(checkStyle(check.status)),
          el("span", { class: "id", text: check.id }),
        ]),
        el("div", { class: "meta", text: facts.join(" · ") }),
      ]);
      if (check.reason) row.append(el("p", { class: "meta", text: check.reason }));
      if ((check.evidence ?? []).length > 0) row.append(evidenceList(check.evidence ?? []));
      list.append(row);
    }
    root.append(list);
  }

  // Verification is its own axis, with its own evidence: a relation never adds up
  // to verified (AC3).
  root.append(el("h2", { class: "sub", text: "Verification" }));
  root.append(el("div", { class: "badges" }, [badge(verificationStyle(goal.verification))]));
  const vrefs = goal.verification_evidence ?? [];
  if (vrefs.length === 0) {
    root.append(el("p", { class: "empty", text: "No verification evidence. Code presence is not verification." }));
  } else {
    root.append(evidenceList(vrefs));
  }

  for (const diagnostic of goal.diagnostics ?? []) {
    root.append(el("p", { class: "diag", text: "! " + diagnostic }));
  }
}

/** renderArea is the module view: what is in it, and which goals depend on it. */
function renderArea(root: HTMLElement, state: State, dir: string): void {
  const goals = state.goals ?? [];
  const links: { goal: Goal; refs: Ref[] }[] = [];
  goals.forEach((goal) => {
    const area = goalAreas(goal).find((candidate) => candidate.dir === dir);
    if (area) links.push({ goal, refs: area.refs });
  });

  root.append(el("div", { class: "crumbs", text: "Project map › " + dir }));
  root.append(el("h2", { text: dir }));
  const statuses = links.map((link) => goalStatus(link.goal));
  const worst = statuses.includes("violated") ? "violated" : statuses.includes("unknown") ? "unknown" : "pass";
  root.append(el("div", { class: "badges" }, [badge(checkStyle(worst))]));

  const files = new Set(links.flatMap((link) => link.refs.map((ref) => ref.path)));
  root.append(
    el("ul", { class: "facts" }, [
      el("li", { text: `${files.size} file${files.size === 1 ? "" : "s"}` }),
      el("li", { text: `${links.length} goal${links.length === 1 ? "" : "s"} linked` }),
    ]),
  );

  for (const link of links) {
    root.append(el("h2", { class: "sub", text: link.goal.label }));
    root.append(el("div", { class: "badges" }, [badge(checkStyle(goalStatus(link.goal)))]));
    root.append(evidenceList(link.refs));
  }
}

function overview(state: State): HTMLElement {
  const goals = state.goals ?? [];
  const areas = new Set<string>();
  let checks = 0;
  const verdicts = { pass: 0, violated: 0, unknown: 0 };
  for (const goal of goals) {
    for (const area of goalAreas(goal)) areas.add(area.dir);
    for (const check of checksOf(goal)) {
      checks++;
      verdicts[check.status]++;
    }
  }
  const changed = (state.diff.files ?? []).length;
  const modules = (state.structure?.modules ?? []).length;
  const edges = (state.structure?.edges ?? []).length;
  return el("ul", { class: "facts" }, [
    el("li", { text: `${modules} module${modules === 1 ? "" : "s"}, ${edges} internal import${edges === 1 ? "" : "s"}` }),
    el("li", { text: `${goals.length} goal${goals.length === 1 ? "" : "s"}` }),
    el("li", { text: `${areas.size} area${areas.size === 1 ? "" : "s"}` }),
    el("li", { text: `${checks} check${checks === 1 ? "" : "s"} · ${verdicts.pass} pass · ${verdicts.violated} violated · ${verdicts.unknown} unknown` }),
    el("li", { text: `${changed} changed file${changed === 1 ? "" : "s"}` }),
  ]);
}

function diffFacts(file: DiffFile): HTMLElement {
  return el("div", { class: "facts" }, [
    el("div", { class: "meta", text: `${file.status} · +${file.added} −${file.removed}` }),
  ]);
}

function linkedGoals(state: State, path: string): HTMLElement {
  const list = el("div");
  const hits: Goal[] = [];
  (state.goals ?? []).forEach((goal) => {
    if (evidenceOf(goal).some((ref) => ref.path === path)) hits.push(goal);
  });
  list.append(el("h2", { class: "sub", text: "Goals linked to this file" }));
  if (hits.length === 0) {
    list.append(el("p", { class: "empty", text: "No goal is linked to this file." }));
    return list;
  }
  const ul = el("ul", { class: "facts" });
  for (const goal of hits) ul.append(el("li", { text: goal.label + " — " + checkStyle(goalStatus(goal)).word }));
  list.append(ul);
  return list;
}

function evidenceList(refs: Ref[]): HTMLElement {
  const list = el("ul", { class: "evidence" });
  for (const ref of refs) {
    const row = el("li", { class: "ev" }, [
      el("div", { class: "ev-head" }, [
        el("span", { class: "kind", text: ref.kind }),
        el("span", { class: "loc", text: locationText(ref.path, ref.line_start, ref.line_end) }),
      ]),
      el("div", { class: "meta", text: `${ref.symbol || "(no symbol)"} · ${ref.provenance} · confidence ${ref.confidence.toFixed(2)}` + (ref.verifier ? ` · ${ref.verifier}` : "") }),
    ]);
    list.append(row);
  }
  return list;
}

export function renderConflicts(root: HTMLElement, state: State): void {
  clear(root);
  const conflicts = state.prompt_conflicts ?? [];
  root.append(el("h2", { text: "Prompt conflicts" }));
  if (conflicts.length === 0) {
    root.append(el("p", { class: "empty", text: state.prompt_shown ? "No conflict with a locked goal." : "No task prompt." }));
    return;
  }
  root.append(el("p", { class: "meta", text: "Shown, never resolved: the locked spec stays the baseline." }));
  for (const conflict of conflicts) {
    root.append(
      el("div", { class: "conflict" }, [
        badge(checkStyle("violated")),
        el("div", { class: "meta", text: "Prompt: “" + conflict.prompt + "”" }),
        el("div", { class: "diag", text: conflict.reason }),
        el("div", { class: "meta", text: conflict.spec_path + " › " + conflict.label }),
      ]),
    );
  }
}

export function renderFooter(root: HTMLElement, state: State): void {
  const cache = state.cache;
  const parts: string[] = [];
  parts.push(cache.state === "disabled" ? "Cache disabled" : `Cache ${cache.state}${cache.path ? ": " + cache.path : ""}`);

  // A repository can produce many diagnostics; the strip shows the first few and
  // counts the rest, so a broken dependency tree cannot swallow the page.
  const diagnostics = state.diagnostics ?? [];
  const shown = diagnostics.slice(0, 5);
  parts.push(...shown);
  if (diagnostics.length > shown.length) parts.push(`… ${diagnostics.length - shown.length} more`);
  root.textContent = parts.join(" · ");
}
