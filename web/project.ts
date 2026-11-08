// The project map: how the repository is built, independent of any goal.
//
// Modules are laid out in dependency layers — a module that other modules import
// sits below them — and import edges are drawn with the path that produced them as
// provenance. The page still decides nothing: sizes, layers, and edges are read
// from the state the server computed, and a module's symbols are listed with the
// line they start on so the reader can drill to the source.

import { clear, el, svgEl } from "./dom";
import { renderFileDiff } from "./panels";
import { Edge, Module, State, Symbol } from "./types";
import type { MapMode } from "./main";

/** moduleKey is the selection key for a module node. */
export const moduleKey = (dir: string): string => "m:" + dir;

export interface ProjectOptions {
  selected: string | null;
  /** focus is the module whose inside is on screen; null shows the whole project. */
  focus: string | null;
  onSelect: (key: string | null) => void;
  onFocus: (dir: string | null) => void;
  /** onMode switches between the project map, the goal map, and the data model. */
  mode: MapMode;
  onMode: (mode: MapMode) => void;
}

/** fileKey is the selection key for one source file inside a module. */
export const fileKey = (path: string): string => "s:" + path;

// Drawing units. The viewBox uses these, so layout is independent of the panel size.
const NODE_H = 54;
const GAP_Y = 74;
const GAP_X = 18;
const GAP = 26;
const PAD = 16;

/** depthOf assigns each module the length of its longest dependency chain. */
export function depthOf(modules: Module[]): Map<string, number> {
  const byDir = new Map(modules.map((m) => [m.dir, m]));
  const depths = new Map<string, number>();

  const walk = (dir: string, seen: Set<string>): number => {
    const known = depths.get(dir);
    if (known != null) return known;
    if (seen.has(dir)) return 0; // an import cycle is depth 0, not a hang
    seen.add(dir);
    const module = byDir.get(dir);
    let depth = 0;
    for (const dep of module?.depends_on ?? []) {
      if (byDir.has(dep)) depth = Math.max(depth, walk(dep, seen) + 1);
    }
    depths.set(dir, depth);
    return depth;
  };

  for (const module of modules) walk(module.dir, new Set<string>());
  return depths;
}

/** A box in the drawing: position and size in viewBox units. */
interface Box {
  x: number;
  y: number;
  w: number;
  h: number;
  row: number;
}

/**
 * gridLayout places n items in rows of at most cols, centring the last row.
 *
 * The drawing is scaled to the panel width, so a long single row shrinks the text
 * to nothing. Wrapping keeps every box at a readable size.
 */
function gridLayout(n: number, cols: number, w: number, h: number, gapY: number, gapX: number) {
  const rows = Math.max(1, Math.ceil(n / cols));
  const width = PAD * 2 + cols * w + (cols - 1) * gapX;
  const height = PAD * 2 + rows * h + (rows - 1) * gapY;
  return {
    width,
    height,
    box(index: number): Box | null {
      if (index >= n) return null;
      const row = Math.floor(index / cols);
      const col = index % cols;
      const inRow = Math.min(cols, n - row * cols);
      const offset = ((cols - inRow) * (w + gapX)) / 2;
      return { x: PAD + offset + col * (w + gapX), y: PAD + row * (h + gapY), w, h, row };
    },
  };
}

/** edgePath routes one arrow: sideways within a row, otherwise downward. */
function edgePath(from: Box, to: Box, w: number, h: number): string {
  if (from.row === to.row) {
    const right = from.x + w;
    const leftTo = to.x;
    const y = from.y + h / 2;
    // Same row: leave the right edge and enter the left edge, so the line does not
    // loop back over its own box.
    return `M ${right} ${y} C ${right + GAP_X / 2} ${y}, ${leftTo - GAP_X / 2} ${to.y + h / 2}, ${leftTo} ${to.y + h / 2}`;
  }
  const x1 = from.x + w / 2;
  const y1 = from.y + h;
  const x2 = to.x + w / 2;
  const y2 = to.y;
  const mid = (y1 + y2) / 2;
  return `M ${x1} ${y1} C ${x1} ${mid}, ${x2} ${mid}, ${x2} ${y2}`;
}

/**
 * renderProject draws the map, its legend, and the view toggle.
 *
 * An empty structure is not an empty panel: it says the analyzer found no source
 * in the V1 language set, so the picture is not mistaken for a broken page.
 */
export function renderProject(container: HTMLElement, state: State, opts: ProjectOptions): void {
  const modules = state.structure?.modules ?? [];
  const edges = state.structure?.edges ?? [];

  clear(container);

  const focused = opts.focus ? modules.find((m) => m.dir === opts.focus) : undefined;
  if (focused) {
    renderFileMap(container, state, focused, opts);
    return;
  }

  if (modules.length === 0) {
    container.append(
      el("p", {
        class: "empty map-empty",
        text: "No source in the supported languages (Go, TypeScript), so there is no construction to draw.",
      }),
    );
    return;
  }

  // Modules are ordered by dependency depth — the ones others depend on last — and
  // laid out in a wrapped grid. A single row of eleven boxes is unreadable once the
  // drawing is scaled to the panel, so rows wrap at three.
  const depths = depthOf(modules);
  const ordered = [...modules].sort((a, b) => {
    const byDepth = (depths.get(b.dir) ?? 0) - (depths.get(a.dir) ?? 0);
    return byDepth !== 0 ? byDepth : a.dir.localeCompare(b.dir);
  });
  const WIDTH = 216;
  const grid = gridLayout(ordered.length, Math.min(3, ordered.length), WIDTH, NODE_H, GAP_Y, GAP_X);

  const boxes = new Map<string, Box>();
  ordered.forEach((module, index) => {
    const box = grid.box(index);
    if (box) boxes.set(module.dir, { ...box, w: WIDTH });
  });

  const svg = svgEl("svg", "project-map");
  svg.setAttribute("viewBox", `0 0 ${grid.width} ${grid.height}`);
  svg.setAttribute("role", "group");
  svg.setAttribute("aria-label", "Project construction: modules and their imports");

  const edgeLayer = svgEl("g", "edges");
  const nodeLayer = svgEl("g", "nodes");
  svg.append(edgeLayer, nodeLayer);

  for (const edge of edges) {
    const from = boxes.get(edge.from);
    const to = boxes.get(edge.to);
    if (!from || !to) continue;
    const path = svgEl("path", "edge");
    path.setAttribute("d", edgePath(from, to, WIDTH, NODE_H));
    const title = svgEl("title");
    title.textContent = `${edge.from} imports ${edge.import}`;
    path.append(title);
    edgeLayer.append(path);
  }

  for (const module of modules) {
    const box = boxes.get(module.dir);
    if (!box) continue;
    nodeLayer.append(node(module, box, opts.selected === moduleKey(module.dir), opts));
  }

  container.append(svg);
  container.append(legend(modules, edges.length));
}

/**
 * renderFileMap draws one module from the inside: its files, the imports that stay
 * within it, and their symbols. This is the level at which "how is this built"
 * becomes concrete, so it is a diagram rather than a list.
 */
function renderFileMap(container: HTMLElement, state: State, module: Module, opts: ProjectOptions): void {
  if (module.files.length === 0) {
    container.append(el("p", { class: "empty map-empty", text: "This module has no source files." }));
    return;
  }

  const inside = new Set(module.files);
  const raw = (state.structure?.file_edges ?? []).filter((e) => inside.has(e.from) && inside.has(e.to));
  // One line per pair of files: several symbols used from the same file are one
  // relation, and drawing them as parallel curves makes the picture unreadable.
  const edges: { from: string; to: string; kind: "import" | "reference"; labels: string[] }[] = [];
  for (const edge of raw) {
    const label = edge.kind === "reference" ? (edge.symbol ?? "") : (edge.import ?? "");
    const existing = edges.find((e) => e.from === edge.from && e.to === edge.to);
    if (existing) {
      if (edge.kind === "import") existing.kind = "import";
      if (label) existing.labels.push(label);
      continue;
    }
    edges.push({ from: edge.from, to: edge.to, kind: edge.kind ?? "import", labels: label ? [label] : [] });
  }
  const deps = new Map<string, string[]>();
  for (const path of module.files) deps.set(path, []);
  for (const edge of edges) deps.get(edge.from)?.push(edge.to);

  // Files are ordered by how deep their dependency chain is, then by name, and laid
  // out in rows: deep dependencies end up below the files that import them.
  const depth = new Map<string, number>();
  const walk = (path: string, seen: Set<string>): number => {
    const known = depth.get(path);
    if (known != null) return known;
    if (seen.has(path)) return 0;
    seen.add(path);
    let value = 0;
    for (const dep of deps.get(path) ?? []) value = Math.max(value, walk(dep, seen) + 1);
    depth.set(path, value);
    return value;
  };
  for (const path of module.files) walk(path, new Set<string>());

  const symbolsOf = (path: string): Symbol[] => module.symbols.filter((s) => s.path === path);
  const ordered = [...module.files].sort((a, b) => {
    const byDepth = (depth.get(b) ?? 0) - (depth.get(a) ?? 0);
    return byDepth !== 0 ? byDepth : a.localeCompare(b);
  });

  const W = 210;
  const H = 46;
  const COLS = Math.min(4, ordered.length);
  const grid = gridLayout(ordered.length, COLS, W, H, GAP, GAP);
  const boxes = new Map<string, Box>();
  ordered.forEach((path, index) => {
    const box = grid.box(index);
    if (box) boxes.set(path, box);
  });

  const svg = svgEl("svg", "project-map file-map");
  svg.setAttribute("viewBox", `0 0 ${grid.width} ${grid.height}`);
  svg.setAttribute("role", "group");
  svg.setAttribute("aria-label", `${module.dir}: files and their imports`);
  const edgeLayer = svgEl("g", "edges");
  const nodeLayer = svgEl("g", "nodes");
  svg.append(edgeLayer, nodeLayer);

  for (const edge of edges) {
    const from = boxes.get(edge.from);
    const to = boxes.get(edge.to);
    if (!from || !to) continue;
    const path = svgEl("path", "edge");
    path.setAttribute("d", edgePath(from, to, W, H));
    if (edge.kind === "reference") path.setAttribute("class", "edge ref");
    const title = svgEl("title");
    title.textContent =
      edge.kind === "import"
        ? `${edge.from} imports ${edge.labels.join(", ")}`
        : `${edge.from} uses ${edge.labels.join(", ")} from ${edge.to}`;
    path.append(title);
    edgeLayer.append(path);
  }

  for (const path of ordered) {
    const box = boxes.get(path);
    if (!box) continue;
    nodeLayer.append(fileNode(path, box, symbolsOf(path), opts.selected === fileKey(path), opts.onSelect));
  }

  container.append(svg);
  const testFiles = module.files.filter((p) => /(\.test\.tsx?|\.spec\.tsx?|_test\.go)$/.test(p)).length;
  const imports = edges.filter((e) => e.kind === "import").length;
  const references = edges.length - imports;
  container.append(
    el("p", {
      class: "legend",
      text:
        `${module.files.length} files · ${module.symbols.length} symbols · ${testFiles} test file${testFiles === 1 ? "" : "s"} · ` +
        `${imports} ${imports === 1 ? "import" : "imports"} (solid) · ${references} ${references === 1 ? "symbol use" : "symbol uses"} (dashed)`,
    }),
  );
}

/** fileNode draws one file with its symbol count; selecting it shows its symbols. */
function fileNode(
  path: string,
  box: { x: number; y: number },
  symbols: Symbol[],
  selected: boolean,
  onSelect: (key: string | null) => void,
): SVGElement {
  const group = svgEl("g", "module file" + (selected ? " selected" : ""));
  group.setAttribute("transform", `translate(${box.x} ${box.y})`);
  group.setAttribute("tabindex", "0");
  group.setAttribute("role", "button");
  const name = path.split("/").pop() ?? path;
  const tests = symbols.filter((s) => s.test).length;
  group.setAttribute("aria-label", `${path}: ${symbols.length - tests} symbols, ${tests} test symbols`);

  const rect = svgEl("rect", "module-box");
  rect.setAttribute("width", "210");
  rect.setAttribute("height", "46");
  rect.setAttribute("rx", "8");
  group.append(rect);

  const label = svgEl("text", "module-name");
  label.setAttribute("x", "12");
  label.setAttribute("y", "20");
  label.textContent = name;
  group.append(label);

  const meta = svgEl("text", "module-meta");
  meta.setAttribute("x", "12");
  meta.setAttribute("y", "36");
  meta.textContent = `${Math.max(0, symbols.length - tests)} symbols` + (tests ? ` · ${tests} test` : "");
  group.append(meta);

  const title = svgEl("title");
  title.textContent = [path, ...symbols.slice(0, 12).map((s) => `  ${s.name}:${s.line}`)].join("\n");
  group.append(title);

  const activate = (): void => onSelect(fileKey(path));
  group.addEventListener("click", activate);
  group.addEventListener("keydown", (event) => {
    if ((event as KeyboardEvent).key === "Enter" || (event as KeyboardEvent).key === " ") {
      event.preventDefault();
      activate();
    }
  });
  return group;
}

/**
 * renderMapBar draws the breadcrumb and the view toggle.
 *
 * It lives outside the two maps because the toggle must stay reachable from both:
 * rendering it inside the project map made the goal map a one-way door.
 */
export function renderMapBar(
  container: HTMLElement,
  opts: { mode: MapMode; focus: string | null; onMode: (mode: MapMode) => void; onFocus: (dir: string | null) => void },
): void {
  clear(container);
  const project = el("button", { class: "crumb" + (opts.focus ? "" : " on"), text: "Project" });
  project.addEventListener("click", () => opts.onFocus(null));
  container.append(project);
  if (opts.focus) {
    container.append(el("span", { class: "crumb-sep", text: "›" }));
    container.append(el("span", { class: "crumb on", text: opts.focus }));
  }
  container.append(toggle(opts));
}

/** metaText is the second line of a node: weight, tests, and outside dependencies. */
function metaText(module: Module): string {
  const files = module.files.filter((p) => !/(\.test\.tsx?|\.spec\.tsx?|_test\.go)$/.test(p)).length;
  const tests = module.files.length - files;
  const symbols = module.symbols.filter((s) => !s.test).length;
  const external = (module.imports ?? []).length;
  return [
    `${files} file${files === 1 ? "" : "s"}`,
    `${symbols} symbols`,
    tests ? `${tests} test` : "",
    external ? `${external} ext` : "",
  ]
    .filter(Boolean)
    .join(" · ");
}

/** node draws one module: its path, its weight, and its outside dependencies. */
function node(
  module: Module,
  box: { x: number; y: number; w: number },
  selected: boolean,
  opts: ProjectOptions,
): SVGElement {
  const group = svgEl("g", "module" + (selected ? " selected" : ""));
  group.setAttribute("transform", `translate(${box.x} ${box.y})`);
  group.setAttribute("tabindex", "0");
  group.setAttribute("role", "button");
  group.setAttribute("aria-label", `${module.dir}: ${module.files.length} files, ${module.symbols.length} symbols`);

  const rect = svgEl("rect", "module-box");
  rect.setAttribute("width", String(box.w));
  rect.setAttribute("height", String(NODE_H));
  rect.setAttribute("rx", "8");
  group.append(rect);

  const name = svgEl("text", "module-name");
  name.setAttribute("x", "12");
  name.setAttribute("y", "21");
  name.textContent = module.dir;
  group.append(name);

  const detail = svgEl("text", "module-meta");
  detail.setAttribute("x", "12");
  detail.setAttribute("y", "40");
  detail.textContent = metaText(module);
  group.append(detail);

  const title = svgEl("title");
  title.textContent = [
    module.dir,
    ...module.files.map((p) => "  " + p),
    (module.imports ?? []).length ? "external: " + (module.imports ?? []).join(", ") : "",
  ]
    .filter(Boolean)
    .join("\n");
  group.append(title);

  // Selecting a module shows its detail and takes the map inside it: the whole
  // project is only the picture you start from.
  const activate = (): void => {
    opts.onSelect(moduleKey(module.dir));
    opts.onFocus(module.dir);
  };
  group.addEventListener("click", activate);
  group.addEventListener("keydown", (event) => {
    if ((event as KeyboardEvent).key === "Enter" || (event as KeyboardEvent).key === " ") {
      event.preventDefault();
      activate();
    }
  });
  return group;
}

/** legend names the layers and the arrow direction, so the picture is readable. */
function legend(modules: Module[], edgeCount: number): HTMLElement {
  const external = modules.reduce((sum, m) => sum + (m.imports ?? []).length, 0);
  return el("p", {
    class: "legend",
    text:
      `${modules.length} modules · ${edgeCount} internal ${edgeCount === 1 ? "import" : "imports"} · ` +
      `${external} external ${external === 1 ? "dependency" : "dependencies"} · an arrow points from an importer to what it imports`,
  });
}

/** toggle switches between the project map and the goal map. */
export function toggle(opts: { mode: MapMode; onMode: (mode: MapMode) => void }): HTMLElement {
  const group = el("div", { class: "modes", role: "group", ariaLabel: "Map view" });
  const button = (mode: MapMode, label: string): HTMLElement => {
    const b = el("button", {
      class: "mode" + (opts.mode === mode ? " on" : ""),
      text: label,
      ariaPressed: opts.mode === mode ? "true" : "false",
    });
    b.addEventListener("click", () => opts.onMode(mode));
    return b;
  };
  group.append(button("project", "Project"), button("goals", "Goals"), button("data", "Data"));
  return group;
}

/** visibleSymbols is how many names one file shows before the rest are counted. */
const visibleSymbols = 6;

/** symbolText lists a file's symbols with their lines, counting the rest. */
function symbolText(symbols: Symbol[]): string {
  if (symbols.length === 0) return "no declarations";
  const shown = symbols
    .slice(0, visibleSymbols)
    .map((s) => `${s.name}:${s.line}${s.test ? " (test)" : ""}${s.complexity ? ` c${s.complexity}` : ""}`);
  const rest = symbols.length - shown.length;
  return shown.join("  ") + (rest > 0 ? `  +${rest} more` : "");
}

/** tableKey is the selection key for one table of the data model. */
export const tableKey = (name: string): string => "t:" + name;

/**
 * renderTableDetail shows one table as the migrations leave it: its columns, the
 * tables it references, and the migration that declared each relation. The shape is
 * derived from DDL, so every line resolves to a file a reader can open (AC18).
 */
export function renderTableDetail(
  container: HTMLElement,
  state: State,
  name: string,
  onSelect?: (key: string) => void,
): void {
  const table = (state.schema?.tables ?? []).find((t) => t.name === name);
  clear(container);
  container.scrollTop = 0;
  if (!table) {
    container.append(el("p", { class: "empty", text: "That table is no longer in the data model." }));
    return;
  }

  container.append(el("h2", { text: table.name }));
  container.append(
    el("p", { class: "muted", text: `declared by ${table.migration}` }),
  );
  if (table.unknown) {
    container.append(el("p", { class: "cycle", text: `Not fully modelled · ${table.unknown}` }));
  }

  const columns = el("ul", { class: "files" });
  for (const column of table.columns) {
    columns.append(
      el("li", {}, [
        el("div", { class: "file-path", text: `${column.name}${column.key ? "  (key)" : ""}` }),
        el("div", { class: "muted", text: column.type }),
      ]),
    );
  }
  container.append(el("h3", { text: "Columns" }), columns);

  container.append(el("h3", { text: "References" }));
  if (table.references.length === 0) {
    container.append(el("p", { class: "empty", text: "No foreign key leaves this table." }));
  } else {
    const refs = el("ul", { class: "files" });
    for (const ref of table.references) {
      const target = ref.to.split(".")[0];
      const open = el("button", { class: "outline-file", text: `${ref.from} → ${ref.to}` });
      open.type = "button";
      if (onSelect) open.addEventListener("click", () => onSelect(tableKey(target)));
      else open.disabled = true;
      refs.append(
        el("li", {}, [
          open,
          el("div", { class: "muted", text: `${ref.constraint ? ref.constraint + " · " : ""}${ref.migration}` }),
        ]),
      );
    }
    container.append(refs);
  }
}

/**
 * renderFileDetail lists one file's symbols with their lines, and the imports that
 * point at it and away from it, so a node in the diagram resolves to source.
 */
export function renderFileDetail(container: HTMLElement, state: State, path: string, showDiff = true): void {
  clear(container);
  container.scrollTop = 0;

  const module = (state.structure?.modules ?? []).find((m) => (m.files ?? []).includes(path));
  const symbols = (module?.symbols ?? []).filter((s) => s.path === path);
  container.append(el("h2", { text: path.split("/").pop() ?? path }));
  container.append(el("div", { class: "crumbs", text: module ? module.dir + " › " : "" }));
  if (!module) {
    container.append(el("p", { class: "empty", text: "That file is no longer in the analysis." }));
    return;
  }
  container.append(
    el("p", {
      class: "muted",
      text: `${symbols.filter((s) => !s.test).length} symbols · ${symbols.filter((s) => s.test).length} test symbols`,
    }),
  );

  const list = el("ul", { class: "files" });
  for (const symbol of symbols) {
    list.append(el("li", { class: "file-path", text: `${symbol.name}:${symbol.line}${symbol.test ? " (test)" : ""}` }));
  }
  container.append(list);
  if (symbols.length === 0) container.append(el("p", { class: "muted", text: "No declarations in this file." }));

  renderFileDiff(container, state, path, showDiff);

  const edges = state.structure?.file_edges ?? [];
  const imports = edges.filter((e) => e.from === path && e.kind !== "reference");
  const uses = edges.filter((e) => e.from === path && e.kind === "reference");
  const importedBy = edges.filter((e) => e.to === path && e.kind !== "reference");
  const usedBy = edges.filter((e) => e.to === path && e.kind === "reference");
  if (imports.length) {
    container.append(el("h3", { text: "Imports" }));
    const deps = el("ul", { class: "deps" });
    for (const edge of imports) deps.append(el("li", { text: `${edge.import} → ${edge.to}` }));
    container.append(deps);
  }
  if (uses.length) {
    container.append(el("h3", { text: "Uses from this module" }));
    container.append(depsByFile(uses, (edge) => edge.to, (edge) => edge.symbol ?? ""));
  }
  if (importedBy.length) {
    container.append(el("h3", { text: "Imported by" }));
    const users = el("ul", { class: "deps" });
    for (const edge of importedBy) users.append(el("li", { text: edge.from }));
    container.append(users);
  }
  if (usedBy.length) {
    container.append(el("h3", { text: "Used by" }));
    container.append(depsByFile(usedBy, (edge) => edge.from, (edge) => edge.symbol ?? ""));
  }
}

/**
 * depsByFile groups relations by the file on the other side, so one file with many
 * used symbols is one line instead of twenty.
 */
function depsByFile(edges: Edge[], file: (edge: Edge) => string, name: (edge: Edge) => string): HTMLElement {
  const byFile = new Map<string, string[]>();
  for (const edge of edges) {
    const key = file(edge);
    const names = byFile.get(key) ?? [];
    if (name(edge)) names.push(name(edge));
    byFile.set(key, names);
  }
  const list = el("ul", { class: "deps" });
  for (const [path, names] of [...byFile.entries()].sort((a, b) => a[0].localeCompare(b[0]))) {
    list.append(el("li", { text: names.length ? `${path}: ${names.join(", ")}` : path }));
  }
  return list;
}

/**
 * renderModuleDetail shows what a module contains: its files, the symbols in them
 * with their lines, and the dependencies that resolve nowhere in this repository.
 */
export function renderModuleDetail(container: HTMLElement, state: State, dir: string): void {
  const module = (state.structure?.modules ?? []).find((m) => m.dir === dir);
  clear(container);
  // The panel keeps its scroll offset across selections; a module detail that opens
  // mid-list reads as a truncated file list.
  container.scrollTop = 0;
  if (!module) {
    container.append(el("p", { class: "empty", text: "That module is no longer in the analysis." }));
    return;
  }

  container.append(el("h2", { text: module.dir }));
  container.append(
    el("p", {
      class: "muted",
      text: `${module.files.length} files · ${module.symbols.filter((s) => !s.test).length} symbols · ${module.symbols.filter((s) => s.test).length} test symbols`,
    }),
  );

  // Coupling is counted from the module edges: a fact about the construction, and
  // the number a reader needs before deciding whether a change is local (AC16).
  container.append(
    el("p", {
      class: "muted",
      text: `imports ${module.fan_out} · imported by ${module.fan_in} · instability ${module.instability.toFixed(2)}`,
    }),
  );
  if (module.in_cycle) {
    container.append(el("p", { class: "cycle", text: "Part of an import cycle: this module is reachable from itself." }));
  }

  const byFile = new Map<string, Symbol[]>();
  for (const symbol of module.symbols) {
    const list = byFile.get(symbol.path) ?? [];
    list.push(symbol);
    byFile.set(symbol.path, list);
  }

  const files = el("ul", { class: "files" });
  for (const path of module.files) {
    const symbols = byFile.get(path) ?? [];
    const item = el("li", {}, [
      el("div", { class: "file-path", text: path }),
      el("div", { class: "file-symbols", text: symbolText(symbols) }),
    ]);
    files.append(item);
  }
  container.append(files);
  if (module.symbols_truncated) {
    container.append(el("p", { class: "muted", text: "Symbol lists are capped per file; the count above is complete, the names are not." }));
  }

  if ((module.depends_on ?? []).length) {
    container.append(el("h3", { text: "Imports" }));
    const list = el("ul", { class: "deps" });
    for (const dep of module.depends_on ?? []) list.append(el("li", { text: dep }));
    container.append(list);
  }
  if ((module.imports ?? []).length) {
    container.append(el("h3", { text: "External dependencies" }));
    const list = el("ul", { class: "deps" });
    for (const dep of module.imports ?? []) list.append(el("li", { text: dep }));
    container.append(list);
  }
}
