const SCROLLBAR_WIDTH_PROPERTY = "--document-scrollbar-width";
const PAGE_OWNER_SELECTOR = "[data-page-scroll-owner]";

let lockCount = 0;
let releaseLockedOwners: (() => void) | null = null;

/** The one scroll element used by standard document routes. */
export function documentScrollOwner(doc: Document = document): HTMLElement {
  return doc.body;
}

/**
 * Measures the gutter the body reserves. A forced overflow probe can disagree
 * with `scrollbar-gutter: stable` (headless Chromium, overlay scrollbars), and
 * bounded routes remove this exact gutter.
 */
export function installDocumentScrollPolicy(doc: Document = document): number {
  const width = doc.body.offsetWidth - doc.body.clientWidth;
  doc.documentElement.style.setProperty(SCROLLBAR_WIDTH_PROPERTY, `${width}px`);
  return width;
}

/**
 * The element that scrolls the page around `element`: the nearest marked page
 * owner whose CSS makes it scroll (the Admin body at 901px and up), else the body.
 */
export function pageScrollOwner(element: HTMLElement): HTMLElement {
  for (let node = element.parentElement; node; node = node.parentElement) {
    if (!node.matches(PAGE_OWNER_SELECTOR)) continue;
    const { overflowY } = getComputedStyle(node);
    if (overflowY === "auto" || overflowY === "scroll") return node;
  }
  return documentScrollOwner(element.ownerDocument);
}

/** Offset from the body scroll owner's content origin. */
export function documentOffsetTop(
  element: HTMLElement,
  owner: HTMLElement = documentScrollOwner(element.ownerDocument),
): number {
  return Math.round(
    element.getBoundingClientRect().top - owner.getBoundingClientRect().top + owner.scrollTop,
  );
}

export function resetDocumentScroll(doc: Document = document): void {
  documentScrollOwner(doc).scrollTop = 0;
}

/** Locks every page-level owner (nestable). Unmarked modal scrollers stay scrollable. */
export function lockPageScroll(doc: Document = document): () => void {
  if (lockCount++ === 0) {
    const owners = [
      documentScrollOwner(doc),
      ...doc.querySelectorAll<HTMLElement>(PAGE_OWNER_SELECTOR),
    ];
    const previous = owners.map((owner) => ({ owner, overflow: owner.style.overflow }));
    owners.forEach((owner) => {
      owner.style.overflow = "hidden";
    });
    releaseLockedOwners = () => {
      previous.forEach(({ owner, overflow }) => {
        owner.style.overflow = overflow;
      });
    };
  }

  let released = false;
  return () => {
    if (released) return;
    released = true;
    if (--lockCount === 0) {
      releaseLockedOwners?.();
      releaseLockedOwners = null;
    }
  };
}
