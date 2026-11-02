// The left column is navigation, not a second copy of the diagram.
//
// It lists the project's modules and, once a module is chosen, its files; below
// that, the changed files of the selected diff. Every entry is a way into the
// picture on the right, so the two panels never show the same thing twice.

import { clear, el } from "./dom";
import { fileKey, moduleKey } from "./project";
import { State } from "./types";

export interface NavOptions {
  selected: string | null;
  focus: string | null;
  onSelect: (key: string | null) => void;
  onFocus: (dir: string | null) => void;
}

const isTestFile = (path: string): boolean => /(\.test\.tsx?|\.spec\.tsx?|_test\.go)$/.test(path);

export function renderNav(root: HTMLElement, state: State, opts: NavOptions): void {
  clear(root);

  const modules = state.structure?.modules ?? [];
  const changed = state.diff.files ?? [];
  const changedPaths = new Set(changed.map((f) => f.path));

  const head = el("div", { class: "section-head" }, [
    el("h2", { text: "Outline" }),
    el("span", {
      class: "meta",
      text: `${modules.length} module${modules.length === 1 ? "" : "s"} · ${changed.length} changed`,
    }),
  ]);
  root.append(head);

  if (modules.length === 0) {
    root.append(el("p", { class: "empty", text: "No source in the supported languages, so there is nothing to outline." }));
  }

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
        text: `${files.length} file${files.length === 1 ? "" : "s"}` + (touched ? ` · ${touched} changed` : ""),
      }),
    );
    button.addEventListener("click", () => {
      opts.onSelect(moduleKey(module.dir));
      opts.onFocus(module.dir);
    });
    row.append(button);

    // Only the focused module lists its files: an always-expanded tree would be the
    // diagram again, in text.
    if (focused) {
      const inner = el("ul", { class: "outline-files" });
      for (const path of files) {
        const touchedHere = changedPaths.has(path);
        const item = el("li", {});
        const entry = el("button", {
          class: "outline-file" + (opts.selected === fileKey(path) ? " on" : "") + (touchedHere ? " touched" : ""),
          text: path.split("/").pop() ?? path,
          title: path,
          ariaPressed: opts.selected === fileKey(path) ? "true" : "false",
        });
        entry.type = "button";
        if (isTestFile(path)) entry.append(el("span", { class: "tag", text: "test" }));
        if (touchedHere) entry.append(el("span", { class: "tag changed", text: "changed" }));
        entry.addEventListener("click", () => opts.onSelect(fileKey(path)));
        item.append(entry);
        inner.append(item);
      }
      row.append(inner);
    }
    list.append(row);
  }
  root.append(list);

  // Changed source files are already in the outline above, marked as changed.
  // Only changes outside the source tree appear here, so no file is listed twice.
  const inModules = new Set(modules.flatMap((m) => m.files ?? []));
  const others = changed.filter((file) => !inModules.has(file.path));
  if (changed.length === 0) {
    root.append(el("p", { class: "empty", text: "The working tree is clean." }));
    return;
  }
  if (others.length === 0) return;
  root.append(
    el("div", { class: "section-head" }, [el("h2", { text: "Other changes" })]),
  );
  const changes = el("ul", { class: "outline changes" });
  for (const file of others) {
    const item = el("li", {});
    const entry = el("button", {
      class: "outline-file" + (opts.selected === "f:" + file.path ? " on" : ""),
      text: file.path,
      title: file.path,
      ariaPressed: opts.selected === "f:" + file.path ? "true" : "false",
    });
    entry.type = "button";
    entry.append(el("span", { class: "meta", text: `${file.status} · +${file.added} −${file.removed}` }));
    entry.addEventListener("click", () => opts.onSelect("f:" + file.path));
    item.append(entry);
    changes.append(item);
  }
  root.append(changes);
}
