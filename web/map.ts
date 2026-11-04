// The project map: goals on the left, one node per code area on the right, with
// the linked files listed inside a node when it is expanded.
//
// One node per area, not one per goal-area pair: an area two goals share is one
// part of the project, and drawing it twice splits the picture. Layout is
// deterministic and size-only — a goal sits beside the block of areas it links, so
// the whole project stays legible as one picture and drill-in never hides the rest
// of it (.agents/specs/design-system.md, whole-project visibility). Nothing here
// decides a status; the shapes only show what the server said (architecture D5).

import { Check, Goal, Ref, State } from "./types";
import { checkStyle, svgEl } from "./dom";

export interface MapOptions {
  selected: string | null;
  expanded: Set<string>;
  showDiff: boolean;
  onSelect: (key: string) => void;
  onToggle: (key: string) => void;
}

export const goalKey = (i: number): string => `g:${i}`;
export const areaKey = (dir: string): string => `a:${dir}`;
export const fileKey = (path: string): string => `f:${path}`;

const COLS = { goal: 0, area: 288 };
const WIDTH = { goal: 252, area: 344 };
const HEIGHT = { goal: 58, area: 46, row: 20 };
/** ROW_TOP clears the label and sub lines, so an expanded node never overlaps itself. */
const ROW_TOP = 54;
const GAP = { area: 10, goal: 22 };
const TOTAL_WIDTH = COLS.area + WIDTH.area + 12;
const MAX_ROWS = 8;

/** dirOf is the module grouping: an area is the directory an evidence path lives in. */
export function dirOf(path: string): string {
  const cut = path.lastIndexOf("/");
  return cut < 0 ? "." : path.slice(0, cut);
}

function short(text: string, max: number): string {
  return text.length <= max ? text : text.slice(0, max - 1) + "…";
}

export interface Area {
  dir: string;
  refs: Ref[];
  goals: number[];
}

/** checksOf is a goal's checks, or none: a goal may declare no check at all. */
export function checksOf(goal: Goal): Check[] {
  return goal.checks ?? [];
}

/**
 * goalStatus is the worst verdict among a goal's checks, and unknown when it has
 * none. The server decides every verdict; this only picks the one that needs
 * attention first, so a failing check is never hidden behind a passing one.
 */
export function goalStatus(goal: Goal): string {
  let status = "";
  for (const check of checksOf(goal)) {
    if (check.status === "violated") return "violated";
    if (check.status === "unknown") status = "unknown";
    if (check.status === "pass" && !status) status = "pass";
  }
  return status || "unknown";
}

/** areasOf collects one entry per directory across every check's evidence. */
export function areasOf(state: State): Area[] {
  const order: string[] = [];
  const byDir = new Map<string, Area>();
  (state.goals ?? []).forEach((goal, index) => {
    for (const ref of evidenceOf(goal)) {
      const dir = dirOf(ref.path);
      let area = byDir.get(dir);
      if (!area) {
        area = { dir, refs: [], goals: [] };
        byDir.set(dir, area);
        order.push(dir);
      }
      area.refs.push(ref);
      if (!area.goals.includes(index)) area.goals.push(index);
    }
  });
  return order.map((dir) => byDir.get(dir) as Area);
}

/** evidenceOf is every reference the goal's checks rest on. */
export function evidenceOf(goal: Goal): Ref[] {
  return checksOf(goal).flatMap((check) => check.evidence ?? []);
}

/** goalAreas groups a goal's evidence by directory, preserving evidence order. */
export function goalAreas(goal: Goal): { dir: string; refs: Ref[] }[] {
  const order: string[] = [];
  const byDir = new Map<string, Ref[]>();
  for (const ref of evidenceOf(goal)) {
    const dir = dirOf(ref.path);
    let list = byDir.get(dir);
    if (!list) {
      list = [];
      byDir.set(dir, list);
      order.push(dir);
    }
    list.push(ref);
  }
  return order.map((dir) => ({ dir, refs: byDir.get(dir) as Ref[] }));
}

/** areaStatus is the worst verdict among the goals that point at this area. */
export function areaStatus(state: State, area: Area): string {
  let status = "";
  for (const index of area.goals) {
    const goal = (state.goals ?? [])[index];
    if (!goal) continue;
    const candidate = goalStatus(goal);
    if (candidate === "violated") return "violated";
    if (candidate === "unknown") status = "unknown";
    if (candidate === "pass" && !status) status = "pass";
  }
  return status || "unknown";
}

interface Box {
  key: string;
  x: number;
  y: number;
  w: number;
  h: number;
  cls: string;
  icon: string;
  label: string;
  sub: string;
  title: string;
  column: string;
  rows: { text: string; changed: boolean }[];
  mutable: boolean;
}

export function layout(state: State, expanded: Set<string>, showDiff: boolean): {
  boxes: Box[];
  edges: { from: Box; to: Box }[];
  height: number;
} {
  const changed = new Set((state.diff.files ?? []).map((file) => file.path));
  const boxes: Box[] = [];
  const edges: { from: Box; to: Box }[] = [];
  const areas = areasOf(state);
  // An invariant is not a goal: it has no place in the goal map (AC14).
  const goals = (state.goals ?? []).map((goal, index) => ({ goal, index })).filter((entry) => !entry.goal.invariant);

  // Areas first: they own the vertical space, and each goal is placed beside its
  // own block afterwards.
  const areaBoxes = new Map<string, Box>();
  let y = 0;
  for (const area of areas) {
    const key = areaKey(area.dir);
    const open = expanded.has(key);
    const files = uniqueRefs(area.refs);
    const diffHere = files.filter((ref) => changed.has(ref.path)).length;
    const rows = open
      ? files.slice(0, MAX_ROWS).map((ref) => ({
          text: rowText(ref, area.dir),
          changed: showDiff && changed.has(ref.path),
        }))
      : [];
    const extra = open && files.length > MAX_ROWS ? 1 : 0;
    const height = rows.length || extra ? ROW_TOP + (rows.length + extra) * HEIGHT.row + 2 : HEIGHT.area;
    const box: Box = {
      key,
      x: COLS.area,
      y,
      w: WIDTH.area,
      h: height,
      cls: "node-area " + checkStyle(areaStatus(state, area)).cls + (open ? " open" : ""),
      icon: files.length ? (open ? "▾" : "▸") : "·",
      label: short(area.dir, 38),
      sub:
        `${files.length} file${files.length === 1 ? "" : "s"} · ${area.goals.length} goal${area.goals.length === 1 ? "" : "s"}` +
        (diffHere && showDiff ? ` · ${diffHere} changed` : ""),
      title: `${area.dir} — ${files.length} linked file(s)`,
      column: "area",
      rows,
      mutable: files.length > 0,
    };
    (box as Box & { overflow?: number }).overflow = open && files.length > MAX_ROWS ? files.length - MAX_ROWS : 0;
    boxes.push(box);
    areaBoxes.set(area.dir, box);
    y += height + GAP.area;
  }

  // Goals: beside their block of areas, or on the next free line when unlinked.
  let free = 0;
  let previousGoal: Box | null = null;
  goals.forEach(({ goal, index }) => {
    const style = checkStyle(goalStatus(goal));
    const checks = checksOf(goal);
    const dirs = goalAreas(goal).map((area) => area.dir);
    const linked = dirs.map((dir) => areaBoxes.get(dir)).filter((box): box is Box => !!box);
    let top: number;
    if (linked.length) {
      top = linked[0].y + linked[0].h / 2 - HEIGHT.goal / 2;
    } else {
      top = Math.max(free, previousGoal ? previousGoal.y + previousGoal.h + 8 : 0);
    }
    free = Math.max(free, top + HEIGHT.goal + 8);

    const box: Box = {
      key: goalKey(index),
      x: COLS.goal,
      y: top,
      w: WIDTH.goal,
      h: HEIGHT.goal,
      cls: "node-goal " + style.cls + (linked.length ? "" : " unlinked"),
      icon: style.icon,
      label: short(goal.label, 34),
      sub:
        (checks.length ? `${style.word} · ${checks.length} check${checks.length === 1 ? "" : "s"}` : "No check defined") +
        (linked.length ? "" : " · no code"),
      title: `${goal.heading.join(" › ")} — ${goal.label}`,
      column: "goal",
      rows: [],
      mutable: false,
    };
    boxes.push(box);
    for (const target of linked) edges.push({ from: box, to: target });
    previousGoal = box;
  });

  const height = Math.max(y, free, 160);
  return { boxes, edges, height };
}

function uniqueRefs(refs: Ref[]): Ref[] {
  const seen = new Set<string>();
  const out: Ref[] = [];
  for (const ref of refs) {
    if (seen.has(ref.path)) continue;
    seen.add(ref.path);
    out.push(ref);
  }
  return out;
}

function rowText(ref: Ref, dir: string): string {
  const name = dir === "." || !ref.path.startsWith(dir + "/") ? ref.path : ref.path.slice(dir.length + 1);
  const symbol = ref.symbol || "—";
  return short(`${name} · ${symbol} · ${ref.line_start}`, 46);
}

export function renderMap(container: HTMLElement, state: State, opts: MapOptions): void {
  if ((state.goals ?? []).length === 0) {
    // No baseline means no map, and the emptiness says why: an unexplained blank
    // panel reads as a broken page.
    const note = document.createElement("p");
    note.className = "empty map-empty";
    note.textContent =
      state.baseline.state === "conflicting"
        ? "Goal alignment unresolved: no map until one locked spec claims the baseline."
        : "Goal alignment unavailable: no locked spec, so there is nothing to map. Browsing stays available.";
    container.replaceChildren(note);
    return;
  }
  const { boxes, edges, height } = layout(state, opts.expanded, opts.showDiff);
  const svg = svgEl("svg", "map") as SVGSVGElement;
  svg.setAttribute("viewBox", `0 0 ${TOTAL_WIDTH} ${height}`);
  svg.setAttribute("width", String(TOTAL_WIDTH));
  svg.setAttribute("height", String(height));
  svg.setAttribute("role", "tree");
  svg.setAttribute("aria-label", "Goal to code map");

  const edgeLayer = svgEl("g", "edges");
  for (const edge of edges) {
    const path = svgEl("path", "edge");
    const x1 = edge.from.x + edge.from.w;
    const y1 = edge.from.y + edge.from.h / 2;
    const x2 = edge.to.x;
    const y2 = edge.to.y + edge.to.h / 2;
    path.setAttribute("d", `M ${x1} ${y1} C ${x1 + 34} ${y1}, ${x2 - 34} ${y2}, ${x2} ${y2}`);
    edgeLayer.append(path);
  }
  svg.append(edgeLayer);

  const nodeLayer = svgEl("g", "nodes");
  for (const box of boxes) {
    const group = svgEl("g", "node " + box.cls + (opts.selected === box.key ? " selected" : "")) as SVGGElement;
    group.setAttribute("transform", `translate(${box.x} ${box.y})`);
    group.setAttribute("tabindex", "0");
    group.setAttribute("role", "treeitem");
    group.setAttribute("aria-selected", opts.selected === box.key ? "true" : "false");
    group.setAttribute("aria-label", box.title);
    group.dataset.key = box.key;
    group.dataset.column = box.column;

    const rect = svgEl("rect");
    rect.setAttribute("width", String(box.w));
    rect.setAttribute("height", String(box.h));
    rect.setAttribute("rx", "8");
    group.append(rect);

    const icon = svgEl("text", "icon");
    icon.setAttribute("x", "12");
    icon.setAttribute("y", "22");
    icon.textContent = box.icon;
    group.append(icon);

    const label = svgEl("text", "label");
    label.setAttribute("x", "28");
    label.setAttribute("y", "22");
    label.textContent = box.label;
    group.append(label);

    const sub = svgEl("text", "sub");
    sub.setAttribute("x", "28");
    sub.setAttribute("y", "38");
    sub.textContent = box.sub;
    group.append(sub);

    box.rows.forEach((row, i) => {
      const line = svgEl("text", "row" + (row.changed ? " changed" : ""));
      line.setAttribute("x", "28");
      line.setAttribute("y", String(ROW_TOP + i * HEIGHT.row));
      line.textContent = (row.changed ? "◆ " : "") + row.text;
      group.append(line);
    });
    const overflow = (box as Box & { overflow?: number }).overflow ?? 0;
    if (overflow > 0) {
      const more = svgEl("text", "row more");
      more.setAttribute("x", "28");
      more.setAttribute("y", String(ROW_TOP + MAX_ROWS * HEIGHT.row));
      more.textContent = `… ${overflow} more`;
      group.append(more);
    }

    group.addEventListener("click", (event) => {
      event.stopPropagation();
      if (box.mutable) opts.onToggle(box.key);
      opts.onSelect(box.key);
    });
    group.addEventListener("keydown", (event) => {
      const key = (event as KeyboardEvent).key;
      if (key === "Enter" || key === " ") {
        event.preventDefault();
        if (box.mutable) opts.onToggle(box.key);
        opts.onSelect(box.key);
      }
      if (key === "ArrowDown" || key === "ArrowUp") {
        event.preventDefault();
        moveFocus(box.column, group, key === "ArrowDown" ? 1 : -1);
      }
    });
    nodeLayer.append(group);
  }
  svg.append(nodeLayer);

  container.replaceChildren(svg);
}

/** moveFocus walks nodes in one column, keeping the map usable from the keyboard. */
function moveFocus(column: string, current: SVGGElement, delta: number): void {
  const svg = current.ownerSVGElement;
  if (!svg) return;
  const peers = Array.from(svg.querySelectorAll<SVGGElement>(`g.node[data-column="${column}"]`));
  const at = peers.indexOf(current);
  const next = peers[at + delta];
  if (next) next.focus();
}
