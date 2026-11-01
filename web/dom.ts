// Small DOM helpers. Everything is built with text nodes, never innerHTML: labels,
// paths, and diagnostics come from a repository and must never be parsed as markup.

export interface Attrs {
  class?: string;
  text?: string;
  title?: string;
  role?: string;
  tabindex?: number;
  ariaLabel?: string;
  ariaPressed?: string;
  hidden?: boolean;
}

export function el<K extends keyof HTMLElementTagNameMap>(
  tag: K,
  attrs: Attrs = {},
  children: (Node | string)[] = [],
): HTMLElementTagNameMap[K] {
  const node = document.createElement(tag);
  if (attrs.class) node.className = attrs.class;
  if (attrs.text != null) node.textContent = attrs.text;
  if (attrs.title) node.title = attrs.title;
  if (attrs.role) node.setAttribute("role", attrs.role);
  if (attrs.tabindex != null) node.tabIndex = attrs.tabindex;
  if (attrs.ariaLabel) node.setAttribute("aria-label", attrs.ariaLabel);
  if (attrs.ariaPressed) node.setAttribute("aria-pressed", attrs.ariaPressed);
  if (attrs.hidden) node.hidden = true;
  for (const child of children) node.append(child);
  return node;
}

export const svgEl = (tag: string, cls?: string): SVGElement => {
  const node = document.createElementNS("http://www.w3.org/2000/svg", tag);
  if (cls) node.setAttribute("class", cls);
  return node;
};

/** clear empties a container without touching focus twice. */
export function clear(node: Element): void {
  while (node.firstChild) node.removeChild(node.firstChild);
}

/** Status vocabulary: shape, icon, and word together, so colour is never alone. */
export interface StatusStyle {
  icon: string;
  word: string;
  cls: string;
}

export function relationStyle(relation: string): StatusStyle {
  switch (relation) {
    case "appears-to-support":
      return { icon: "▲", word: "Appears to support", cls: "st-support" };
    case "conflict":
      return { icon: "⚠", word: "Conflict", cls: "st-conflict" };
    case "unmapped":
      return { icon: "◌", word: "Unmapped", cls: "st-unmapped" };
    case "stale":
      return { icon: "⏱", word: "Stale", cls: "st-stale" };
    case "unavailable":
      return { icon: "?", word: "Unavailable", cls: "st-unavailable" };
    default:
      return { icon: "?", word: "Unknown", cls: "st-unknown" };
  }
}

export function verificationStyle(verification: string): StatusStyle {
  switch (verification) {
    case "verified":
      return { icon: "✓", word: "Verified", cls: "st-verified" };
    case "failed":
      return { icon: "⚠", word: "Verification failed", cls: "st-conflict" };
    case "unavailable":
      return { icon: "?", word: "Verification unavailable", cls: "st-unavailable" };
    default:
      return { icon: "?", word: "Not verified", cls: "st-unknown" };
  }
}

export function badge(style: StatusStyle): HTMLElement {
  return el("span", { class: "badge " + style.cls, text: style.icon + " " + style.word });
}

/** lineRange renders "path:12-18" with the numbers monospaced. */
export function locationText(path: string, start: number, end: number): string {
  if (!start) return path;
  return end > start ? `${path}:${start}-${end}` : `${path}:${start}`;
}
