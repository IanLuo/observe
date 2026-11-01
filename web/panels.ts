// Header, live strip, goal rail, diff list, detail panel, conflicts, and footer.
// Each function renders server-decided values and nothing else: a badge always
// carries the server's word for the state (architecture D5).

import {
  badge,
  clear,
  el,
  locationText,
  relationStyle,
  verificationStyle,
  type StatusStyle,
} from "./dom";
import { dirOf, fileKey, goalAreas, goalKey } from "./map";
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

export function renderHeader(root: HTMLElement, state: State, h: PanelHandlers): void {
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
  diff.setAttribute("aria-pressed", state.diff.files && state.diff.files.length ? "true" : "false");
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

export function renderRail(root: HTMLElement, state: State, selected: string | null, h: PanelHandlers): void {
  clear(root);
  const goals = state.goals ?? [];
  if (goals.length === 0) {
    root.append(el("p", { class: "empty", text: state.baseline.state === "available" ? "No goals in the locked spec." : "No goals: goal alignment unavailable." }));
    return;
  }
  goals.forEach((goal, i) => {
    const key = goalKey(i);
    const item = el("button", {
      class: "rail-item" + (selected === key ? " selected" : ""),
      ariaPressed: selected === key ? "true" : "false",
      ariaLabel: `${goal.label} — ${relationStyle(goal.relation).word}`,
    });
    item.type = "button";
    item.title = goal.heading.join(" › ") + " — " + goal.label;
    item.append(
      el("span", { class: "rail-head" }, [
        el("span", { class: "label", text: goal.label }),
        badge(relationStyle(goal.relation)),
      ]),
      el("span", {
        class: "meta",
        text: `${goalAreas(goal).length} area${goalAreas(goal).length === 1 ? "" : "s"} · ${(goal.evidence ?? []).length} link${(goal.evidence ?? []).length === 1 ? "" : "s"} · ${verificationStyle(goal.verification).word.toLowerCase()}`,
      }),
    );
    item.addEventListener("click", () => h.onSelect(key));
    item.addEventListener("keydown", (event) => {
      const keys = Array.from(root.querySelectorAll<HTMLButtonElement>("button.rail-item"));
      const at = keys.indexOf(item);
      if (event.key === "ArrowDown" && keys[at + 1]) {
        event.preventDefault();
        keys[at + 1].focus();
      }
      if (event.key === "ArrowUp" && keys[at - 1]) {
        event.preventDefault();
        keys[at - 1].focus();
      }
    });
    root.append(item);
  });
}

export function renderDiff(root: HTMLElement, state: State, showDiff: boolean, h: PanelHandlers): void {
  clear(root);
  const files = state.diff.files ?? [];
  root.append(
    el("div", { class: "section-head" }, [
      el("h2", { text: "Selected diff" }),
      el("span", { class: "meta", text: `${files.length} file${files.length === 1 ? "" : "s"} · ${state.diff.hash ? state.diff.hash.slice(0, 12) : "no hash"}` }),
    ]),
  );
  if (files.length === 0) {
    root.append(el("p", { class: "empty", text: "No selected diff: the working tree is clean." }));
    return;
  }
  if (!showDiff) {
    root.append(el("p", { class: "empty", text: "Diff highlight hidden. Status is unchanged by this toggle." }));
  }
  for (const file of files) {
    const box = el("div", { class: "diff-file" });
    box.append(
      el("button", {
        class: "diff-head" + (showDiff ? " changed" : ""),
        ariaLabel: `${file.status} ${file.path}, ${file.added} added, ${file.removed} removed`,
        title: file.path,
      }, [
        el("span", { class: "path", text: file.path }),
        el("span", { class: "meta", text: `${file.status} · +${file.added} −${file.removed}` }),
      ]),
    );
    const head = box.firstElementChild as HTMLButtonElement;
    head.type = "button";
    head.addEventListener("click", () => h.onSelect(fileKey(file.path)));
    if (showDiff && file.lines && file.lines.length) {
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
      box.append(lines);
    }
    root.append(box);
  }
}

export function renderDetail(root: HTMLElement, state: State, selected: string | null): void {
  clear(root);
  const goals = state.goals ?? [];

  if (!selected) {
    root.append(el("h2", { text: "Detail" }));
    root.append(el("p", { class: "empty", text: "Select a goal, an area, or a changed file." }));
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
  root.append(el("div", { class: "badges" }, [badge(relationStyle(goal.relation))]));

  root.append(
    el("p", { class: "meta", text: goal.inferred ? "Relation inferred from the selected diff; the spec text is unchanged." : "Goal read from the locked spec." }),
  );

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

  root.append(el("h2", { class: "sub", text: "Evidence" }));
  const refs = goal.evidence ?? [];
  if (refs.length === 0) {
    root.append(el("p", { class: "empty", text: "No evidence links this goal to code." }));
  } else {
    root.append(evidenceList(refs));
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
  const relations = links.map((link) => link.goal.relation);
  const order = ["conflict", "appears-to-support", "unmapped", "stale", "unavailable"] as const;
  const relation = order.find((candidate) => relations.includes(candidate)) ?? "unavailable";
  root.append(el("div", { class: "badges" }, [badge(relationStyle(relation))]));

  const files = new Set(links.flatMap((link) => link.refs.map((ref) => ref.path)));
  root.append(
    el("ul", { class: "facts" }, [
      el("li", { text: `${files.size} file${files.size === 1 ? "" : "s"}` }),
      el("li", { text: `${links.length} goal${links.length === 1 ? "" : "s"} linked` }),
    ]),
  );

  for (const link of links) {
    root.append(el("h2", { class: "sub", text: link.goal.label }));
    root.append(el("div", { class: "badges" }, [badge(relationStyle(link.goal.relation))]));
    root.append(evidenceList(link.refs));
  }
}

function overview(state: State): HTMLElement {
  const goals = state.goals ?? [];
  const areas = new Set<string>();
  let links = 0;
  for (const goal of goals) {
    for (const area of goalAreas(goal)) areas.add(area.dir);
    links += (goal.evidence ?? []).length;
  }
  const changed = (state.diff.files ?? []).length;
  return el("ul", { class: "facts" }, [
    el("li", { text: `${goals.length} goal${goals.length === 1 ? "" : "s"}` }),
    el("li", { text: `${areas.size} area${areas.size === 1 ? "" : "s"}` }),
    el("li", { text: `${links} evidence link${links === 1 ? "" : "s"}` }),
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
    if ((goal.evidence ?? []).some((ref) => ref.path === path)) hits.push(goal);
  });
  list.append(el("h2", { class: "sub", text: "Goals linked to this file" }));
  if (hits.length === 0) {
    list.append(el("p", { class: "empty", text: "No goal is linked to this file." }));
    return list;
  }
  const ul = el("ul", { class: "facts" });
  for (const goal of hits) ul.append(el("li", { text: goal.label + " — " + relationStyle(goal.relation).word }));
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
        badge(relationStyle("conflict")),
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
  for (const diagnostic of state.diagnostics ?? []) parts.push(diagnostic);
  root.textContent = parts.join(" · ");
}
