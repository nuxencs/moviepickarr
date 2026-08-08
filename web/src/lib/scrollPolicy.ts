const SCROLLBAR_WIDTH_PROPERTY = "--document-scrollbar-width";
const PAGE_OWNER_SELECTOR = "[data-page-scroll-owner]";

let lockCount = 0;
let releaseLockedOwners: (() => void) | null = null;

/** The one scroll element used by standard document routes. */
export function documentScrollOwner(doc: Document = document): HTMLElement {
  return doc.body;
}

/**
 * Measure the browser's native scrollbar without applying author scrollbar
 * styles. Overlay scrollbars report zero. Fixed chrome reads the resulting CSS
 * property so it uses the same inline edge as the document owner.
 */
export function installDocumentScrollPolicy(doc: Document = document): number {
  const probe = doc.createElement("div");
  probe.style.cssText =
    "position:absolute;inset:-9999px auto auto -9999px;width:100px;height:100px;overflow:scroll;visibility:hidden;contain:strict";
  doc.body.append(probe);
  const width = probe.offsetWidth - probe.clientWidth;
  probe.remove();
  doc.documentElement.style.setProperty(SCROLLBAR_WIDTH_PROPERTY, `${width}px`);
  return width;
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

/** Route changes start the shared body owner at its top. */
export function resetDocumentScroll(doc: Document = document): void {
  documentScrollOwner(doc).scrollTop = 0;
}

/**
 * Lock every page-level owner as one nested operation. Modal-owned scrollers
 * are not marked with data-page-scroll-owner and remain available.
 */
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
