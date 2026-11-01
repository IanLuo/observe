// The project map: how the repository is built, independent of any goal.
//
// Modules are laid out in dependency layers — a module that other modules import
// sits below them — and import edges are drawn with the path that produced them as
// provenance. The page still decides nothing: sizes, layers, and edges are read
// from the state the server computed, and a module's symbols are listed with the
// line they start on so the reader can drill to the source.

import { clear, el, svgEl } from "./dom";
import { Module, State, Symbol } from "./types";

/** moduleKey is the selection key for a module node. */
export const moduleKey = (dir: string): string => "m:" + dir;

export interface ProjectOptions {
  selected: string | null;
  onSelect: (key: string | null) => void;
  /** onMode switches between the project map and the goal map. */
  mode: "project" | "goals";
  onMode: (mode: "project" | "goals") => void;
}

interface Layer {
  depth: number;
  modules: Module[];
}

const NODE_H = 54;
const GAP_Y = 74;
const GAP_X = 18;
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

/** layersOf groups modules by dependency depth, deepest last. */
export function layersOf(modules: Module[]): Layer[] {
  const depths = depthOf(modules);
  const layers: Layer[] = [];
  for (const module of modules) {
    const depth = depths.get(module.dir) ?? 0;
    let layer = layers.find((l) => l.depth === depth);
    if (!layer) {
      layer = { depth, modules: [] };
      layers.push(layer);
    }
    layer.modules.push(module);
  }
  // Entered modules sit at the top and their dependencies below them, so the page
  // reads top-down as "this is built from this".
  layers.sort((a, b) => b.depth - a.depth);
  for (const layer of layers) layer.modules.sort((a, b) => a.dir.localeCompare(b.dir));
  return layers;
}

/** nodeWidth sizes a module box by its two lines of text, so nothing is clipped. */
function nodeWidth(module: Module): number {
  const longest = Math.max(module.dir.length * 7.8, metaText(module).length * 5.9);
  return Math.max(160, Math.min(340, 24 + longest));
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
  container.append(toggle(opts));

  if (modules.length === 0) {
    container.append(
      el("p", {
        class: "empty map-empty",
        text: "No source in the supported languages (Go, TypeScript), so there is no construction to draw.",
      }),
    );
    return;
  }

  const layers = layersOf(modules);
  const widest = Math.max(...layers.map((layer) => layer.modules.reduce((sum, m) => sum + nodeWidth(m) + GAP_X, -GAP_X)));
  const width = widest + PAD * 2;

  // Position every module before drawing edges: an edge needs both endpoints.
  const boxes = new Map<string, { x: number; y: number; w: number }>();
  layers.forEach((layer, row) => {
    const rowWidth = layer.modules.reduce((sum, m) => sum + nodeWidth(m) + GAP_X, -GAP_X);
    let x = PAD + Math.max(0, (widest - rowWidth) / 2);
    const y = PAD + row * (NODE_H + GAP_Y);
    // Entry points read best at the top, so deeper dependencies sit lower.
    for (const module of layer.modules) {
      const w = nodeWidth(module);
      boxes.set(module.dir, { x, y, w });
      x += w + GAP_X;
    }
  });
  const maxDepth = Math.max(...layers.map((l) => l.depth));
  const svgHeight = PAD * 2 + (maxDepth + 1) * (NODE_H + GAP_Y) - GAP_Y;

  const svg = svgEl("svg", "project-map");
  svg.setAttribute("viewBox", `0 0 ${Math.ceil(width)} ${Math.ceil(svgHeight)}`);
  svg.setAttribute("role", "group");
  svg.setAttribute("aria-label", "Project construction: modules and their imports");

  const edgeLayer = svgEl("g", "edges");
  const nodeLayer = svgEl("g", "nodes");
  svg.append(edgeLayer, nodeLayer);

  for (const edge of edges) {
    const to = boxes.get(edge.from);
    const from = boxes.get(edge.to);
    if (!to || !from) continue;
    // Importers sit above the modules they import, so an arrow points downward.
    const x1 = to.x + to.w / 2;
    const y1 = to.y + NODE_H;
    const x2 = from.x + from.w / 2;
    const y2 = from.y;
    const midY = (y1 + y2) / 2;
    const path = svgEl("path", "edge");
    path.setAttribute("d", `M ${x1} ${y1} C ${x1} ${midY}, ${x2} ${midY}, ${x2} ${y2}`);
    const title = svgEl("title");
    title.textContent = `${edge.from} imports ${edge.import}`;
    path.append(title);
    edgeLayer.append(path);
  }

  for (const module of modules) {
    const box = boxes.get(module.dir);
    if (!box) continue;
    nodeLayer.append(node(module, box, opts.selected === moduleKey(module.dir), opts.onSelect));
  }

  container.append(svg);
  container.append(legend(modules, edges.length));
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
  onSelect: (key: string | null) => void,
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

  const activate = (): void => onSelect(moduleKey(module.dir));
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
export function toggle(opts: { mode: "project" | "goals"; onMode: (mode: "project" | "goals") => void }): HTMLElement {
  const group = el("div", { class: "modes", role: "group", ariaLabel: "Map view" });
  const button = (mode: "project" | "goals", label: string): HTMLElement => {
    const b = el("button", {
      class: "mode" + (opts.mode === mode ? " on" : ""),
      text: label,
      ariaPressed: opts.mode === mode ? "true" : "false",
    });
    b.addEventListener("click", () => opts.onMode(mode));
    return b;
  };
  group.append(button("project", "Project"), button("goals", "Goals"));
  return group;
}

/** visibleSymbols is how many names one file shows before the rest are counted. */
const visibleSymbols = 6;

/** symbolText lists a file's symbols with their lines, counting the rest. */
function symbolText(symbols: Symbol[]): string {
  if (symbols.length === 0) return "no declarations";
  const shown = symbols.slice(0, visibleSymbols).map((s) => `${s.name}:${s.line}${s.test ? " (test)" : ""}`);
  const rest = symbols.length - shown.length;
  return shown.join("  ") + (rest > 0 ? `  +${rest} more` : "");
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
