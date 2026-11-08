// Entry point: fetch the state contract, render it, and send the two mutations.
// The page owns selection, expansion, and the diff toggle; it owns no status.

import { Envelope, State } from "./types";
import { dirOf, renderMap } from "./map";
import { renderFileDetail, renderFlowDetail, renderMapBar, renderModuleDetail, renderProject, renderTableDetail } from "./project";
import { renderEr } from "./er";
import { renderNav } from "./nav";
import { installResize } from "./resize";
import {
  renderBaseline,
  renderConflicts,
  renderDetail,
  renderFooter,
  renderHeader,
} from "./panels";

/** MapMode is which picture the middle panel draws. */
export type MapMode = "project" | "goals" | "data";

const token = document.querySelector<HTMLMetaElement>("meta[name=observe-token]")?.content ?? "";

interface View {
  state: State | null;
  /** mode decides which map is drawn; the project is the default view. */
  mode: MapMode;
  /** focus is the module the map has drilled into; null is the whole project. */
  focus: string | null;
  selected: string | null;
  expanded: Set<string>;
  showDiff: boolean;
  pending: boolean;
  error: string | null;
}

const view: View = {
  state: null,
  mode: "project" as MapMode,
  focus: null,
  selected: null,
  expanded: new Set<string>(),
  showDiff: true,
  pending: false,
  error: null,
};

const nodes = {
  header: byId("header"),
  baseline: byId("baseline"),
  nav: byId("nav"),
  map: byId("map"),
  mapbar: byId("mapbar"),
  gutter: byId("gutter"),
  detail: byId("detail"),
  conflicts: byId("conflicts"),
  footer: byId("footer"),
  banner: byId("banner"),
};

function byId(id: string): HTMLElement {
  const node = document.getElementById(id);
  if (!node) throw new Error("missing #" + id);
  return node;
}

const handlers = {
  onSelect: (key: string | null) => {
    view.selected = view.selected === key ? null : key;
    render();
  },
  onToggle: (key: string) => {
    if (view.expanded.has(key)) view.expanded.delete(key);
    else view.expanded.add(key);
  },
  onRefresh: () => void mutate("/api/refresh"),
  onClearCache: () => void mutate("/api/cache/clear"),
  onMode: (mode: MapMode) => {
    view.mode = mode;
    view.focus = null;
    view.selected = null;
    render();
  },
  onFocus: (dir: string | null) => {
    view.focus = dir;
    if (dir === null) view.selected = null;
    render();
  },
  onToggleDiff: () => {
    view.showDiff = !view.showDiff;
    render();
  },
};

function render(): void {
  const state = view.state;
  if (!state) {
    nodes.banner.textContent = view.error ?? "Loading the analysis…";
    nodes.banner.className = "banner" + (view.error ? " warn" : "");
    return;
  }

  nodes.banner.hidden = !view.error;
  nodes.banner.textContent = view.error ?? "";
  nodes.banner.className = "banner" + (view.error ? " warn" : "");

  if (view.selected && !selectionExists(state, view.selected)) view.selected = null;

  const shown: State = view.pending ? { ...state, freshness: "updating" } : state;
  renderHeader(nodes.header, shown, view.showDiff, handlers);
  renderBaseline(nodes.baseline, state);
  renderNav(nodes.nav, state, {
    selected: view.selected,
    focus: view.focus,
    onSelect: handlers.onSelect,
    onFocus: handlers.onFocus,
  });
  if (view.selected?.startsWith("s:")) renderFileDetail(nodes.detail, state, view.selected.slice(2), view.showDiff);
  else if (view.selected?.startsWith("m:")) renderModuleDetail(nodes.detail, state, view.selected.slice(2));
  else if (view.selected?.startsWith("t:")) renderTableDetail(nodes.detail, state, view.selected.slice(2), handlers.onSelect);
  else if (view.selected?.startsWith("r:")) renderFlowDetail(nodes.detail, state, view.selected.slice(2), handlers.onSelect);
  else renderDetail(nodes.detail, state, view.selected, view.showDiff, view.mode);
  renderConflicts(nodes.conflicts, state);
  renderFooter(nodes.footer, state);

  const buttons = nodes.header.querySelectorAll("button");
  if (view.pending) for (const button of buttons) button.disabled = true;

  renderMapBar(nodes.mapbar, {
    mode: view.mode,
    focus: view.focus,
    onMode: handlers.onMode,
    onFocus: handlers.onFocus,
  });

  if (view.mode === "data") {
    renderEr(nodes.map, state, { selected: view.selected, onSelect: handlers.onSelect });
  } else if (view.mode === "project") {
    renderProject(nodes.map, state, {
      selected: view.selected,
      focus: view.focus,
      onSelect: handlers.onSelect,
      onFocus: handlers.onFocus,
      mode: view.mode,
      onMode: handlers.onMode,
    });
  } else {
    renderMap(nodes.map, state, {
      selected: view.selected,
      expanded: view.expanded,
      showDiff: view.showDiff,
      onSelect: handlers.onSelect,
      onToggle: handlers.onToggle,
    });
  }
}

/**
 * selectionExists says whether a selection still names something in this state.
 * Every key shape is read as its own kind: a file, an area directory, or a goal
 * index. Reading an area key as a goal index drops the selection on the next
 * render, which showed the module detail for one frame and then lost it.
 */
function selectionExists(state: State, key: string): boolean {
  const value = key.slice(2);
  if (key.startsWith("m:")) return (state.structure?.modules ?? []).some((module) => module.dir === value);
  if (key.startsWith("s:")) {
    return (state.structure?.modules ?? []).some((module) => (module.files ?? []).includes(value));
  }
  if (key.startsWith("f:")) return (state.diff.files ?? []).some((file) => file.path === value);
  if (key.startsWith("t:")) return (state.schema?.tables ?? []).some((table) => table.name === value);
  if (key.startsWith("r:")) return (state.flows ?? []).some((flow) => `${flow.method} ${flow.route}` === value);
  if (key.startsWith("a:")) {
    return (state.goals ?? []).some((goal) =>
      (goal.checks ?? []).some((check) => (check.evidence ?? []).some((ref) => dirOf(ref.path) === value)),
    );
  }
  return (state.goals ?? [])[Number(value)] != null;
}

async function load(): Promise<void> {
  try {
    const response = await fetch("/api/state");
    const body = (await response.json()) as Envelope<State>;
    if (!body.ok) throw new Error(body.error ?? "state unavailable");
    view.state = body.data;
    view.error = null;
  } catch (error) {
    view.error = error instanceof Error ? error.message : String(error);
  }
  render();
  followUp();
}

/**
 * followUp re-reads the state while an analysis is still running.
 *
 * The first analysis can take seconds on a large repository, and the server answers
 * "updating" meanwhile. Without this the page showed an empty picture until the
 * reader pressed Refresh, which reads as a hang.
 */
function followUp(): void {
  if (view.state?.freshness !== "updating") return;
  window.setTimeout(() => void load(), 700);
}

/**
 * refreshOnAttention re-reads the state when the reader comes back to the page.
 *
 * Spec AC6 promises that a view whose data predates an edit never presents itself as
 * current; without this the page sat on a stale picture until Refresh was pressed.
 * It asks the server to analyse again — /api/state only reads what is in memory — and
 * the wait keeps a burst of window events from firing a request each.
 */
const attentionGap = 2000;
let lastAttention = 0;

function refreshOnAttention(): void {
  if (document.visibilityState !== "visible") return;
  if (view.pending || view.state?.freshness === "updating") return;
  const now = Date.now();
  if (now - lastAttention < attentionGap) return;
  lastAttention = now;
  void mutate("/api/refresh");
}

window.addEventListener("focus", refreshOnAttention);
document.addEventListener("visibilitychange", refreshOnAttention);

async function mutate(path: string): Promise<void> {
  view.pending = true;
  render();
  try {
    const response = await fetch(path, {
      method: "POST",
      headers: { "X-Observe-Token": token },
    });
    const body = (await response.json()) as Envelope<State>;
    if (!body.ok) throw new Error(body.error ?? `${path} refused`);
    view.state = body.data;
    view.error = null;
  } catch (error) {
    view.error = error instanceof Error ? error.message : String(error);
  }
  view.pending = false;
  render();
}

document.addEventListener("keydown", (event) => {
  if (event.key !== "Escape") return;
  // Escape walks back out one level: the file, then the module, then nothing.
  if (view.selected) view.selected = null;
  else if (view.focus) view.focus = null;
  else return;
  render();
});

installResize(nodes.gutter);
void load();
