// The data-model map: tables the migrations declare, and the foreign keys between
// them.
//
// This is a second construction next to the module map. It is derived from DDL, not
// from code, so a table here is a statement a migration made — never a table the
// application happens to mention. Layering follows the references: a table that
// other tables point at sits below them, because a row cannot exist before the row
// it references.

import { clear, el, svgEl } from "./dom";
import { State, SchemaTable } from "./types";
import { tableKey } from "./project";

/** ErOptions is what the map needs from the page: the selection and a handler. */
export interface ErOptions {
  selected: string | null;
  onSelect: (key: string | null) => void;
}

const BOX_W = 216;
const BOX_H = 104;
const GAP_Y = 70;
const GAP_X = 26;
const PAD = 16;
// Enough key columns to recognise a table, few enough that the footer still fits.
const visibleColumns = 3;

interface Box {
  x: number;
  y: number;
  w: number;
  h: number;
  row: number;
}

/** depthOf assigns each table the longest chain of references below it. */
function depthOf(tables: SchemaTable[]): Map<string, number> {
  const byName = new Map(tables.map((t) => [t.name, t]));
  const depths = new Map<string, number>();

  const walk = (name: string, seen: Set<string>): number => {
    const known = depths.get(name);
    if (known != null) return known;
    if (seen.has(name)) return 0; // a reference cycle is depth 0, not a hang
    seen.add(name);
    const table = byName.get(name);
    let depth = 0;
    for (const ref of table?.references ?? []) {
      const target = ref.to.split(".")[0];
      if (byName.has(target)) depth = Math.max(depth, walk(target, seen) + 1);
    }
    depths.set(name, depth);
    return depth;
  };

  for (const table of tables) walk(table.name, new Set<string>());
  return depths;
}

/** gridLayout places n items in rows of at most cols, centring the last row. */
function gridLayout(n: number, cols: number) {
  const rows = Math.max(1, Math.ceil(n / cols));
  return {
    width: PAD * 2 + cols * BOX_W + (cols - 1) * GAP_X,
    height: PAD * 2 + rows * BOX_H + (rows - 1) * GAP_Y,
    box(index: number): Box | null {
      if (index >= n) return null;
      const row = Math.floor(index / cols);
      const col = index % cols;
      const inRow = Math.min(cols, n - row * cols);
      const offset = ((cols - inRow) * (BOX_W + GAP_X)) / 2;
      return { x: PAD + offset + col * (BOX_W + GAP_X), y: PAD + row * (BOX_H + GAP_Y), w: BOX_W, h: BOX_H, row };
    },
  };
}

/** refPath routes one foreign key: sideways within a row, otherwise downward. */
function refPath(from: Box, to: Box): string {
  if (from.row === to.row) {
    const right = from.x + from.w;
    const y = from.y + from.h / 2;
    const left = to.x;
    return `M ${right} ${y} C ${right + GAP_X / 2} ${y}, ${left - GAP_X / 2} ${to.y + to.h / 2}, ${left} ${to.y + to.h / 2}`;
  }
  const x1 = from.x + from.w / 2;
  const y1 = from.y + from.h;
  const x2 = to.x + to.w / 2;
  const y2 = to.y;
  const mid = (y1 + y2) / 2;
  return `M ${x1} ${y1} C ${x1} ${mid}, ${x2} ${mid}, ${x2} ${y2}`;
}

/** columnText lists the key columns, which is what makes a box a table. */
function columnText(table: SchemaTable): string[] {
  const keys = table.columns.filter((c) => c.key);
  const shown = (keys.length > 0 ? keys : table.columns).slice(0, visibleColumns);
  const lines = shown.map((c) => `${c.name} ${c.type}`.trim());
  const rest = (keys.length > 0 ? keys.length : table.columns.length) - shown.length;
  if (rest > 0) lines.push(`+${rest} more`);
  return lines;
}

/**
 * renderEr draws the data model: one box per table, keys inside it, and one line per
 * foreign key. A table the reader could not fully model is drawn dashed and says so
 * on screen, so a missing relation is not read as an absent one.
 */
export function renderEr(container: HTMLElement, state: State, opts: ErOptions): void {
  const tables = state.schema?.tables ?? [];
  clear(container);

  if (tables.length === 0) {
    container.append(
      el("p", {
        class: "empty map-empty",
        text: "No SQL migrations in this repository, so there is no declared data model to draw. Tables a running database holds are not read: this view is built from migration files only.",
      }),
    );
    return;
  }

  const depths = depthOf(tables);
  const ordered = [...tables].sort((a, b) => {
    const byDepth = (depths.get(b.name) ?? 0) - (depths.get(a.name) ?? 0);
    return byDepth !== 0 ? byDepth : a.name.localeCompare(b.name);
  });
  const grid = gridLayout(ordered.length, Math.min(3, ordered.length));

  const boxes = new Map<string, Box>();
  ordered.forEach((table, index) => {
    const box = grid.box(index);
    if (box) boxes.set(table.name, box);
  });

  const svg = svgEl("svg", "er-map");
  svg.setAttribute("viewBox", `0 0 ${grid.width} ${grid.height}`);
  svg.setAttribute("role", "group");
  svg.setAttribute("aria-label", "Data model: tables and the foreign keys between them");

  // An arrowhead makes the direction readable: the line leaves the table that holds
  // the foreign key and points at the table it references.
  const defs = svgEl("defs");
  const marker = svgEl("marker", "er-arrow");
  marker.setAttribute("id", "er-arrow");
  marker.setAttribute("viewBox", "0 0 8 8");
  marker.setAttribute("refX", "7");
  marker.setAttribute("refY", "4");
  marker.setAttribute("markerWidth", "6");
  marker.setAttribute("markerHeight", "6");
  marker.setAttribute("orient", "auto-start-reverse");
  const head = svgEl("path");
  head.setAttribute("d", "M 0 0 L 8 4 L 0 8 z");
  head.setAttribute("class", "er-arrow-head");
  marker.append(head);
  defs.append(marker);
  svg.append(defs);

  const edgeLayer = svgEl("g", "edges");
  const nodeLayer = svgEl("g", "nodes");
  svg.append(edgeLayer, nodeLayer);

  let edges = 0;
  for (const table of tables) {
    const from = boxes.get(table.name);
    if (!from) continue;
    for (const ref of table.references) {
      const to = boxes.get(ref.to.split(".")[0]);
      if (!to) continue; // a reference to a table no migration here declares
      edges++;
      const path = svgEl("path", "edge");
      path.setAttribute("d", refPath(from, to));
      path.setAttribute("marker-end", "url(#er-arrow)");
      const title = svgEl("title");
      title.textContent = `${ref.from} references ${ref.to} (${ref.migration})`;
      path.append(title);
      edgeLayer.append(path);
    }
  }

  for (const table of ordered) {
    const box = boxes.get(table.name);
    if (box) nodeLayer.append(tableNode(table, box, opts.selected === tableKey(table.name), opts));
  }

  container.append(svg);
  container.append(legend(tables, edges));
}

/** tableNode draws one table: its name, its keys, and its weight. */
function tableNode(table: SchemaTable, box: Box, selected: boolean, opts: ErOptions): SVGElement {
  const group = svgEl("g", "er-table" + (selected ? " selected" : "") + (table.unknown ? " partial" : ""));
  group.setAttribute("transform", `translate(${box.x} ${box.y})`);
  group.setAttribute("tabindex", "0");
  group.setAttribute("role", "button");
  group.setAttribute("aria-label", `${table.name}: ${table.columns.length} columns, ${table.references.length} references`);

  const rect = svgEl("rect", "er-box");
  rect.setAttribute("width", String(box.w));
  rect.setAttribute("height", String(box.h));
  rect.setAttribute("rx", "8");
  group.append(rect);

  const name = svgEl("text", "module-name");
  name.setAttribute("x", "12");
  name.setAttribute("y", "20");
  name.textContent = table.name;
  group.append(name);

  const header = svgEl("line", "er-rule");
  header.setAttribute("x1", "12");
  header.setAttribute("x2", String(box.w - 12));
  header.setAttribute("y1", "28");
  header.setAttribute("y2", "28");
  group.append(header);

  columnText(table).forEach((line, index) => {
    const text = svgEl("text", "er-column");
    text.setAttribute("x", "12");
    text.setAttribute("y", String(44 + index * 12));
    text.textContent = line;
    group.append(text);
  });

  const count = svgEl("text", "module-meta");
  count.setAttribute("x", "12");
  count.setAttribute("y", String(box.h - 9));
  const refs = table.references.length;
  count.textContent = [
    `${table.columns.length} cols`,
    `${refs} ref${refs === 1 ? "" : "s"}`,
    // The dashed border already says "partial"; the reason lives in the detail panel
    // and in the tooltip, where there is room for it.
    table.unknown ? "partial" : "",
  ]
    .filter(Boolean)
    .join(" · ");
  group.append(count);

  if (table.unknown) {
    const reason = svgEl("title");
    reason.textContent = `Not fully modelled: ${table.unknown}`;
    group.append(reason);
  }

  const open = (): void => opts.onSelect(tableKey(table.name));
  group.addEventListener("click", open);
  group.addEventListener("keydown", (event) => {
    if (event.key === "Enter" || event.key === " ") {
      event.preventDefault();
      open();
    }
  });
  return group;
}

/** legend states what the drawing contains and what it deliberately leaves out. */
function legend(tables: SchemaTable[], edges: number): HTMLElement {
  const partial = tables.filter((t) => t.unknown).length;
  const parts = [
    `${tables.length} table${tables.length === 1 ? "" : "s"}`,
    `${edges} foreign key${edges === 1 ? "" : "s"}`,
    partial ? `${partial} not fully modelled` : "",
  ].filter(Boolean);
  return el("p", {
    class: "legend",
    text: `${parts.join(" · ")}. Derived from migration files, not from a running database.`,
  });
}
