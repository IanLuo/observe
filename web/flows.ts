// The flow map: what a client can call on the left, the tables those calls reach
// on the right, and an edge only where the reader followed the whole chain.
//
// It is drawn from the same flows the outline lists, so a missing edge means the
// reader stopped — not that the route is harmless. Unresolved routes are counted
// below the map and stay in the outline, because a route with no edge is a fact
// worth seeing, not a thing to hide.

import { clear, el, svgEl } from "./dom";
import { State, Flow } from "./types";
import { flowKey, tableKey } from "./project";

/** FlowOptions is what the map needs from the page: the selection and a handler. */
export interface FlowOptions {
  selected: string | null;
  onSelect: (key: string | null) => void;
}

const ROUTE_W = 268;
const TABLE_W = 200;
const ROW_H = 26;
const ROW_GAP = 9;
const COL_GAP = 180;
const PAD = 16;

interface Box {
  x: number;
  y: number;
  w: number;
  h: number;
}

/** labelOf is the route as a reader says it: method, then path. */
const labelOf = (flow: Flow): string => `${flow.method} ${flow.route}`;

/** CHAR_W is the width of one monospace character at the label size, in svg units. */
const CHAR_W = 7;

/**
 * fit shortens a label that would leave its box, keeping both ends so the method
 * and the end of the path stay readable. The full text stays in the node's title.
 */
function fit(text: string, width: number): string {
  const room = Math.floor((width - 24) / CHAR_W);
  if (text.length <= room) return text;
  const head = Math.ceil((room - 1) / 2);
  const tail = Math.floor((room - 1) / 2);
  return text.slice(0, head) + "…" + text.slice(text.length - tail);
}

/**
 * layout stacks boxes in one column. The column is centred against the tallest
 * one, so the two sides meet in the middle instead of hanging from the top.
 */
function layout(count: number, width: number, height: number): { boxes: Box[]; height: number } {
  const boxes: Box[] = [];
  for (let i = 0; i < count; i++) {
    boxes.push({ x: PAD, y: PAD + i * (ROW_H + ROW_GAP), w: width, h: ROW_H });
  }
  const used = PAD * 2 + count * ROW_H + Math.max(0, count - 1) * ROW_GAP;
  return { boxes, height: Math.max(used, height) };
}

/** renderFlows draws the routes that reach a table, and those tables. */
export function renderFlows(container: HTMLElement, state: State, opts: FlowOptions): void {
  const flows = state.flows ?? [];
  const resolved = flows.filter((f) => (f.tables ?? []).length > 0);
  clear(container);

  if (flows.length === 0) {
    container.append(
      el("p", {
        class: "empty map-empty",
        text: "No route registrations were found, so there is no request flow to draw. Routes are read from literal registrations such as app.post('/v1/plans', handler).",
      }),
    );
    return;
  }
  if (resolved.length === 0) {
    container.append(
      el("p", {
        class: "empty map-empty",
        text: `${flows.length} routes were found, and none reached a table this reader could follow. The reasons are in the outline, on each route.`,
      }),
    );
    return;
  }

  // A table more routes reach is a busier table; sorting by that keeps the hubs
  // together instead of scattering them.
  const reached = new Map<string, number>();
  for (const flow of resolved) {
    for (const table of flow.tables ?? []) reached.set(table, (reached.get(table) ?? 0) + 1);
  }
  const tables = [...reached.keys()].sort((a, b) => {
    const byCount = (reached.get(b) ?? 0) - (reached.get(a) ?? 0);
    return byCount !== 0 ? byCount : a.localeCompare(b);
  });
  // Order the routes by the table they reach first, so edges run roughly parallel
  // instead of braiding. Ties fall back to the label, which keeps it stable.
  const tableRank = new Map(tables.map((t, i) => [t, i]));
  const rankOf = (flow: Flow): number =>
    Math.min(...(flow.tables ?? []).map((t) => tableRank.get(t) ?? tables.length));
  const routes = [...resolved].sort((a, b) => {
    const byRank = rankOf(a) - rankOf(b);
    return byRank !== 0 ? byRank : labelOf(a).localeCompare(labelOf(b));
  });

  const left = layout(routes.length, ROUTE_W, 0);
  const right = layout(tables.length, TABLE_W, left.height);

  // Centre the shorter column against the taller one.
  const height = Math.max(left.height, right.height);
  const centre = (boxes: Box[], columnHeight: number): void => {
    const offset = Math.round((height - columnHeight) / 2);
    for (const box of boxes) box.y += Math.max(0, offset);
  };
  centre(left.boxes, left.height);
  centre(right.boxes, right.height);

  const routeBoxes = new Map<string, Box>();
  routes.forEach((flow, i) => routeBoxes.set(labelOf(flow), { ...left.boxes[i], x: PAD }));
  const tableBoxes = new Map<string, Box>();
  tables.forEach((table, i) => {
    const box = right.boxes[i];
    tableBoxes.set(table, { ...box, x: PAD + ROUTE_W + COL_GAP });
  });
  const width = PAD * 2 + ROUTE_W + COL_GAP + TABLE_W;

  const svg = svgEl("svg", "flow-map");
  svg.setAttribute("viewBox", `0 0 ${width} ${height}`);
  svg.setAttribute("role", "group");
  svg.setAttribute("aria-label", "Request flow: routes and the tables they reach");

  // Edges first, so boxes sit above them.
  const edges = svgEl("g", "flow-edges");
  for (const flow of routes) {
    const from = routeBoxes.get(labelOf(flow));
    if (!from) continue;
    for (const table of flow.tables ?? []) {
      const to = tableBoxes.get(table);
      if (!to) continue;
      const x1 = from.x + from.w;
      const y1 = from.y + from.h / 2;
      const x2 = to.x;
      const y2 = to.y + to.h / 2;
      const bend = Math.max(30, (x2 - x1) / 2);
      const path = svgEl("path", "flow-edge" + (opts.selected === flowKey(flow.method, flow.route) ? " on" : ""));
      path.setAttribute("d", `M ${x1} ${y1} C ${x1 + bend} ${y1}, ${x2 - bend} ${y2}, ${x2} ${y2}`);
      edges.append(path);
    }
  }
  svg.append(edges);

  const nodes = svgEl("g", "flow-nodes");
  for (const flow of routes) {
    const box = routeBoxes.get(labelOf(flow));
    if (!box) continue;
    const key = flowKey(flow.method, flow.route);
    const group = svgEl("g", "flow-node route" + (opts.selected === key ? " on" : ""));
    group.setAttribute("tabindex", "0");
    group.setAttribute("role", "button");
    group.setAttribute("aria-label", `${labelOf(flow)}, ${flow.tables?.join(", ") ?? ""}`);
    group.append(svgEl("rect", "box"));
    group.append(svgEl("rect", "accent"));
    const title = svgEl("title", "");
    title.textContent = `${labelOf(flow)} · ${flow.path}:${flow.line}`;
    group.append(title);
    const label = svgEl("text", "flow-route-label");
    label.textContent = fit(labelOf(flow), ROUTE_W);
    group.append(label);
    const pick = (): void => opts.onSelect(key);
    group.addEventListener("click", pick);
    group.addEventListener("keydown", (event) => {
      if (event.key === "Enter" || event.key === " ") {
        event.preventDefault();
        pick();
      }
    });
    place(group, box);
    nodes.append(group);
  }

  for (const table of tables) {
    const box = tableBoxes.get(table);
    if (!box) continue;
    const key = tableKey(table);
    const group = svgEl("g", "flow-node table" + (opts.selected === key ? " on" : ""));
    group.setAttribute("tabindex", "0");
    group.setAttribute("role", "button");
    group.setAttribute("aria-label", `${table}, reached by ${reached.get(table)} routes`);
    group.append(svgEl("rect", "box"));
    const label = svgEl("text", "flow-table-label");
    label.textContent = table;
    group.append(label);
    const count = svgEl("text", "flow-table-count");
    count.textContent = `${reached.get(table)}`;
    group.append(count);
    const pick = (): void => opts.onSelect(key);
    group.addEventListener("click", pick);
    group.addEventListener("keydown", (event) => {
      if (event.key === "Enter" || event.key === " ") {
        event.preventDefault();
        pick();
      }
    });
    place(group, box);
    nodes.append(group);
  }
  svg.append(nodes);

  const wrap = el("div", { class: "flow-map-wrap" });
  wrap.append(svg);
  container.append(wrap);

  const unreached = flows.length - resolved.length;
  container.append(
    el("p", {
      class: "map-foot",
      text:
        `${resolved.length} of ${flows.length} routes reach a table through bindings this reader follows.` +
        (unreached > 0
          ? ` The other ${unreached} reach none of them, or stop at a hop the rules do not describe; each says which in the outline.`
          : ""),
    }),
  );
}

/** place puts a node's shapes at a box. */
function place(group: SVGElement, box: Box): void {
  for (const child of Array.from(group.children)) {
    if (child.tagName === "rect") {
      child.setAttribute("x", String(box.x));
      child.setAttribute("y", String(box.y));
      child.setAttribute("width", String(box.w));
      child.setAttribute("height", String(box.h));
      if (child.classList.contains("accent")) {
        child.setAttribute("width", "3");
      }
    }
    if (child.tagName === "text") {
      const isCount = child.classList.contains("flow-table-count");
      child.setAttribute("x", String(isCount ? box.x + box.w - 12 : box.x + 12));
      child.setAttribute("y", String(box.y + box.h / 2 + 4));
      if (isCount) child.setAttribute("text-anchor", "end");
    }
  }
}
