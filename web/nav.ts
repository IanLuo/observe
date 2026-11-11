// The left column is navigation, not a second copy of the diagram.
//
// It lists the project's modules and, once a module is chosen, its files; below
// that, the changed files of the selected diff. Every entry is a way into the
// picture on the right, so the two panels never show the same thing twice.
//
// Each section is its own function. The outline is where every new analysis lands,
// and one long function had grown to complexity 53 — a number that says the shape
// was wrong, not that the code was careless.

import { badge, checkStyle, clear, el } from "./dom";
import { DiffFile, Module, State } from "./types";
import { fileKey, flowKey, hotspotKey, moduleKey, tableKey } from "./project";

export interface NavOptions {
  selected: string | null;
  focus: string | null;
  onSelect: (key: string | null) => void;
  onFocus: (dir: string | null) => void;
}

const isTestFile = (path: string): boolean => /(\.test\.tsx?|\.spec\.tsx?|_test\.go)$/.test(path);

/** plural writes "1 module", "3 modules". */
const plural = (n: number, one: string, many = one + "s"): string => `${n} ${n === 1 ? one : many}`;

/**
 * stopLabel renders the analyzer's stop code as a few words. The code decides the
 * words; the analyzer never ships prose the reader has to cut up.
 */
function stopLabel(stop?: string): string {
  switch (stop) {
    case "no-store-call":
      return "no store call";
    case "factory-method":
      return "factory has no such method";
    case "factory-unknown":
      return "factory not found";
    case "factory-indirect":
      return "argument re-bound";
    case "factory-argument":
      return "argument is an expression";
    case "store-class":
      return "store is not one class";
    case "store-method":
      return "class has no such method";
    case "unbound-dependency":
      return "injected use case";
    case "no-table":
      return "SQL names no table";
    default:
      return "no table";
  }
}

/** sectionHead is one outline heading, with its count on the right. */
function sectionHead(title: string, meta?: string): HTMLElement {
  const children: HTMLElement[] = [el("h2", { text: title })];
  if (meta) children.push(el("span", { class: "meta", text: meta }));
  return el("div", { class: "section-head" }, children);
}

export function renderNav(root: HTMLElement, state: State, opts: NavOptions): void {
  clear(root);

  const modules = state.structure?.modules ?? [];
  const changed = state.diff.files ?? [];
  root.append(sectionHead("Outline", `${plural(modules.length, "module")} · ${changed.length} changed`));

  renderModules(root, modules, changed, opts);
  renderInvariants(root, state, opts);
  renderArchitecture(root, state, opts);
  renderDataModel(root, state, opts);
  renderFlows(root, state, opts);
  renderOtherChanges(root, modules, changed, opts);
}

/** renderModules lists the project's modules, and the files of the focused one. */
function renderModules(root: HTMLElement, modules: Module[], changed: DiffFile[], opts: NavOptions): void {
  if (modules.length === 0) {
    root.append(el("p", { class: "empty", text: "No source in the supported languages, so there is nothing to outline." }));
  }

  const changedPaths = new Set(changed.map((f) => f.path));
  const list = el("ul", { class: "outline" });
  for (const module of modules) {
    const files = module.files ?? [];
    const touched = files.filter((path) => changedPaths.has(path)).length;
    const focused = opts.focus === module.dir;

    const row = el("li", {});
    const button = el("button", {
      class: "outline-module" + (focused ? " on" : ""),
      ariaPressed: focused ? "true" : "false",
      ariaLabel: `${module.dir}: ${files.length} files, ${touched} changed`,
      text: module.dir,
    });
    button.type = "button";
    button.append(
      el("span", {
        class: "meta",
        text: plural(files.length, "file") + (touched ? ` · ${touched} changed` : ""),
      }),
    );
    button.addEventListener("click", () => {
      opts.onSelect(moduleKey(module.dir));
      opts.onFocus(module.dir);
    });
    row.append(button);

    // Only the focused module lists its files: an always-expanded tree would be the
    // diagram again, in text.
    if (focused) row.append(moduleFiles(files, changedPaths, opts));
    list.append(row);
  }
  root.append(list);
}

/** moduleFiles is the file list of the module the reader opened. */
function moduleFiles(files: string[], changedPaths: Set<string>, opts: NavOptions): HTMLElement {
  const inner = el("ul", { class: "outline-files" });
  for (const path of files) {
    const touchedHere = changedPaths.has(path);
    const selected = opts.selected === fileKey(path);
    const entry = el("button", {
      class: "outline-file" + (selected ? " on" : "") + (touchedHere ? " touched" : ""),
      text: path.split("/").pop() ?? path,
      title: path,
      ariaPressed: selected ? "true" : "false",
    });
    entry.type = "button";
    if (isTestFile(path)) entry.append(el("span", { class: "tag", text: "test" }));
    if (touchedHere) entry.append(el("span", { class: "tag changed", text: "changed" }));
    entry.addEventListener("click", () => opts.onSelect(fileKey(path)));
    inner.append(el("li", {}, [entry]));
  }
  return inner;
}

/**
 * renderInvariants lists checks declared outside the goal spec: layer rules,
 * boundary rules. They are not goals, so they get their own section rather than a
 * place in the goal map (AC14).
 */
function renderInvariants(root: HTMLElement, state: State, opts: NavOptions): void {
  const invariants = (state.goals ?? [])
    .map((goal, index) => ({ goal, index }))
    .filter((entry) => entry.goal.invariant);
  if (invariants.length === 0) return;

  root.append(sectionHead("Invariants", String(invariants.length)));
  const rows = el("ul", { class: "outline invariants" });
  for (const { goal, index } of invariants) {
    const status = (goal.checks ?? [])[0]?.status ?? "unknown";
    const key = `g:${index}`;
    const entry = el("button", {
      class: "outline-file invariant" + (opts.selected === key ? " on" : ""),
      title: goal.label,
      ariaPressed: opts.selected === key ? "true" : "false",
    });
    entry.type = "button";
    entry.append(el("span", { class: "label", text: goal.label }));
    entry.append(badge(checkStyle(status)));
    entry.addEventListener("click", () => opts.onSelect(key));
    rows.append(el("li", {}, [entry]));
  }
  root.append(rows);
}

/**
 * renderArchitecture is counted, not inferred: cycles from the import graph,
 * hotspots from complexity and churn. Both are entry points, not a second diagram
 * (AC16).
 */
function renderArchitecture(root: HTMLElement, state: State, opts: NavOptions): void {
  if (!state.structure?.modules?.length) return;

  const cycles = state.structure?.cycles ?? [];
  const hotspots = state.hotspots ?? [];
  root.append(
    sectionHead(
      "Architecture",
      `${plural(cycles.length, "cycle")} · ${plural(hotspots.length, "hotspot")}`,
    ),
  );

  if (cycles.length === 0) {
    root.append(el("p", { class: "empty", text: "No import cycles among these modules." }));
  } else {
    const list = el("ul", { class: "outline cycles" });
    for (const cycle of cycles) {
      const entry = el("button", {
        class: "outline-file cycle-entry",
        text: cycle.join(" → "),
        title: cycle.join(" → "),
      });
      entry.type = "button";
      entry.addEventListener("click", () => {
        opts.onSelect(moduleKey(cycle[0]));
        opts.onFocus(cycle[0]);
      });
      list.append(el("li", {}, [entry]));
    }
    root.append(list);
  }

  if (hotspots.length > 0) {
    const list = el("ul", { class: "outline hotspots" });
    for (const spot of hotspots) {
      const key = hotspotKey(spot.path, spot.line);
      const entry = el(
        "button",
        {
          class: "outline-file" + (opts.selected === key ? " on" : ""),
          title: `${spot.path}:${spot.line}`,
        },
        [
          el("span", { text: `${spot.symbol} · c${spot.complexity} · ${spot.churn} commits` }),
          el("span", { class: "muted", text: `${spot.path}:${spot.line}` }),
        ],
      );
      entry.type = "button";
      entry.addEventListener("click", () => opts.onSelect(key));
      list.append(el("li", {}, [entry]));
    }
    root.append(list);
  }
}

/**
 * renderDataModel lists the tables the migrations declare. A table the reader could
 * not fully model says so, because a missing relation must not read as an absent
 * one (AC18).
 */
function renderDataModel(root: HTMLElement, state: State, opts: NavOptions): void {
  const tables = state.schema?.tables ?? [];
  if (tables.length === 0) return;

  root.append(sectionHead("Data model", plural(tables.length, "table")));
  const list = el("ul", { class: "outline tables" });
  for (const table of tables) {
    const entry = el("button", {
      class: "outline-file" + (opts.selected === tableKey(table.name) ? " on" : ""),
      title: `${table.name} · ${table.migration}`,
      text: `${table.name} · ${plural(table.columns.length, "col")} · ${plural(table.references.length, "ref")}${
        table.unknown ? " · not fully modelled" : ""
      }`,
    });
    entry.type = "button";
    entry.addEventListener("click", () => opts.onSelect(tableKey(table.name)));
    list.append(el("li", {}, [entry]));
  }
  root.append(list);
}

/**
 * renderFlows lists the routes: what a client can call and what it reaches (AC19).
 * Two groups, because a route that reaches a table and one that stops are different
 * answers. The stop label is a code the analyzer emits; the full sentence stays in
 * the title.
 */
function renderFlows(root: HTMLElement, state: State, opts: NavOptions): void {
  const flows = state.flows ?? [];
  if (flows.length === 0) return;

  const resolved = flows.filter((f) => (f.tables ?? []).length > 0);
  const stopped = flows.filter((f) => (f.tables ?? []).length === 0);
  root.append(
    sectionHead("Flows", `${plural(flows.length, "route")} · ${resolved.length} reach a table`),
  );
  if (resolved.length > 0) flowGroup(root, "Reaches a table", resolved, opts);
  if (stopped.length > 0) flowGroup(root, "Stops before a table", stopped, opts);
}

/** flowGroup is one group of routes under the Flows heading. */
function flowGroup(root: HTMLElement, title: string, flows: State["flows"], opts: NavOptions): void {
  root.append(
    el("div", { class: "section-head sub" }, [
      el("h3", { text: title }),
      el("span", { class: "meta", text: String((flows ?? []).length) }),
    ]),
  );

  const items = el("ul", { class: "outline flows" });
  for (const flow of flows ?? []) {
    const tables = flow.tables ?? [];
    const entry = el("button", {
      class: "outline-file" + (opts.selected === flowKey(flow.method, flow.route) ? " on" : ""),
      title: `${flow.method} ${flow.route} · ${flow.path}:${flow.line}${flow.unknown ? " · " + flow.unknown : ""}`,
      text: `${flow.method} ${flow.route} · ${tables.length ? tables.join(", ") : stopLabel(flow.stop)}`,
    });
    entry.type = "button";
    entry.addEventListener("click", () => opts.onSelect(flowKey(flow.method, flow.route)));
    items.append(el("li", {}, [entry]));
  }
  root.append(items);
}

/**
 * renderOtherChanges lists the changed files that the outline above did not already
 * show, so no file is listed twice.
 */
function renderOtherChanges(root: HTMLElement, modules: Module[], changed: DiffFile[], opts: NavOptions): void {
  if (changed.length === 0) {
    root.append(el("p", { class: "empty", text: "The working tree is clean." }));
    return;
  }
  const inModules = new Set(modules.flatMap((m) => m.files ?? []));
  const others = changed.filter((file) => !inModules.has(file.path));
  if (others.length === 0) return;

  root.append(sectionHead("Other changes"));
  const list = el("ul", { class: "outline changes" });
  for (const file of others) {
    const key = "f:" + file.path;
    const entry = el("button", {
      class: "outline-file" + (opts.selected === key ? " on" : ""),
      text: file.path,
      title: file.path,
      ariaPressed: opts.selected === key ? "true" : "false",
    });
    entry.type = "button";
    entry.append(el("span", { class: "meta", text: `${file.status} · +${file.added} −${file.removed}` }));
    entry.addEventListener("click", () => opts.onSelect(key));
    list.append(el("li", {}, [entry]));
  }
  root.append(list);
}
