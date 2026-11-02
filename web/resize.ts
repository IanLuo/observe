// The detail panel is resizable: diff lines are long, and how much room they need
// depends on the file. The width is a local preference, stored in this browser only
// — nothing about it reaches the server or the analyzed repository.

/** The panel's width is kept within reachable bounds, whatever the drag does. */
export const minWidth = 280;
export const maxWidth = 760;
const step = 24;
const storageKey = "observe.detailWidth";
const property = "--detail-width";

/** clampWidth keeps a width usable: wide enough for a path, narrow enough for a map. */
export function clampWidth(width: number, viewport: number): number {
  // Never more than half the window, so the diagram keeps room at any size.
  const ceiling = Math.min(maxWidth, Math.max(minWidth, Math.round(viewport * 0.5)));
  return Math.min(ceiling, Math.max(minWidth, Math.round(width)));
}

/** applyWidth sets the panel width on the document root. */
export function applyWidth(width: number): void {
  document.documentElement.style.setProperty(property, `${width}px`);
}

/**
 * installResize wires the separator: drag to resize, arrow keys for keyboard use,
 * double-click to restore the default. The separator is a real focusable element, so
 * the panel is adjustable without a mouse.
 */
export function installResize(gutter: HTMLElement): void {
  let width = clampWidth(Number(localStorage.getItem(storageKey)) || 380, window.innerWidth);
  applyWidth(width);

  const set = (next: number): void => {
    width = clampWidth(next, window.innerWidth);
    applyWidth(width);
    gutter.setAttribute("aria-valuenow", String(width));
    try {
      localStorage.setItem(storageKey, String(width));
    } catch {
      // A browser that refuses storage still resizes; only the memory is lost.
    }
  };
  set(width);

  // The drag is measured as a delta from where it started, not from the pointer's
  // absolute position: grabbing the edge of a handle must not snap the panel to the
  // cursor.
  let drag: { pointer: number; startX: number; startWidth: number } | null = null;

  const finish = (): void => {
    if (!drag) return;
    try {
      gutter.releasePointerCapture(drag.pointer);
    } catch {
      // The pointer is already gone; the capture is gone with it.
    }
    drag = null;
    document.body.classList.remove("resizing");
  };

  gutter.addEventListener("pointerdown", (event) => {
    const e = event as PointerEvent;
    if (e.button !== 0) return;
    drag = { pointer: e.pointerId, startX: e.clientX, startWidth: width };
    gutter.setPointerCapture(e.pointerId);
    document.body.classList.add("resizing");
    event.preventDefault();
  });
  gutter.addEventListener("pointermove", (event) => {
    if (!drag) return;
    const e = event as PointerEvent;
    // The panel grows as the pointer moves left, since it is anchored right.
    set(drag.startWidth + (drag.startX - e.clientX));
  });
  gutter.addEventListener("pointerup", finish);
  gutter.addEventListener("pointercancel", finish);
  // A pointer released over another element, or a capture lost to the browser, must
  // end the drag: otherwise plain mouse movement keeps resizing the panel.
  gutter.addEventListener("lostpointercapture", finish);
  window.addEventListener("blur", finish);

  gutter.addEventListener("keydown", (event) => {
    if (event.key === "ArrowLeft") set(width + step);
    else if (event.key === "ArrowRight") set(width - step);
    else if (event.key === "Home") set(minWidth);
    else if (event.key === "End") set(maxWidth);
    else return;
    event.preventDefault();
  });

  gutter.addEventListener("dblclick", () => set(380));
  window.addEventListener("resize", () => set(width));
}
