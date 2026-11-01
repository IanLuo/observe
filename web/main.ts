// Entry point: fetch the state contract, render it, and send the two mutations.
// The page owns selection, expansion, and the diff toggle; it owns no status.

import { Envelope, State } from "./types";
import { dirOf, renderMap } from "./map";
import {
  renderBaseline,
  renderConflicts,
  renderDetail,
  renderDiff,
  renderFooter,
  renderHeader,
  renderRail,
} from "./panels";

const token = document.querySelector<HTMLMetaElement>("meta[name=observe-token]")?.content ?? "";

interface View {
  state: State | null;
  selected: string | null;
  expanded: Set<string>;
  showDiff: boolean;
  pending: boolean;
  error: string | null;
}

const view: View = {
  state: null,
  selected: null,
  expanded: new Set<string>(),
  showDiff: true,
  pending: false,
  error: null,
};

const nodes = {
  header: byId("header"),
  baseline: byId("baseline"),
  rail: byId("rail"),
  diff: byId("diff"),
  map: byId("map"),
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
  renderRail(nodes.rail, state, view.selected, handlers);
  renderDiff(nodes.diff, state, view.showDiff, handlers);
  renderDetail(nodes.detail, state, view.selected);
  renderConflicts(nodes.conflicts, state);
  renderFooter(nodes.footer, state);

  const buttons = nodes.header.querySelectorAll("button");
  if (view.pending) for (const button of buttons) button.disabled = true;

  renderMap(nodes.map, state, {
    selected: view.selected,
    expanded: view.expanded,
    showDiff: view.showDiff,
    onSelect: handlers.onSelect,
    onToggle: handlers.onToggle,
  });
}

/**
 * selectionExists says whether a selection still names something in this state.
 * Every key shape is read as its own kind: a file, an area directory, or a goal
 * index. Reading an area key as a goal index drops the selection on the next
 * render, which showed the module detail for one frame and then lost it.
 */
function selectionExists(state: State, key: string): boolean {
  const value = key.slice(2);
  if (key.startsWith("f:")) return (state.diff.files ?? []).some((file) => file.path === value);
  if (key.startsWith("a:")) {
    return (state.goals ?? []).some((goal) => (goal.evidence ?? []).some((ref) => dirOf(ref.path) === value));
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
}

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
  if (event.key === "Escape" && view.selected) {
    view.selected = null;
    render();
  }
});

void load();
