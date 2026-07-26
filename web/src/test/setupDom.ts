/* ============================================================
   Setup for the "dom" vitest project (see vitest.config.ts).

   Unmounts every rendered tree between tests, and fills the two jsdom gaps
   the app's components hit: matchMedia (the reduced-motion check) and
   HTMLImageElement.decode (the reveal's backdrop handoff). Everything else
   is left as jsdom ships it — a test that needs more should say so itself.
   ============================================================ */

import { cleanup } from "@testing-library/react";
import { afterEach } from "vitest";

afterEach(cleanup);

if (!window.matchMedia) {
  window.matchMedia = ((query: string) => ({
    matches: false,
    media: query,
    onchange: null,
    addListener: () => {},
    removeListener: () => {},
    addEventListener: () => {},
    removeEventListener: () => {},
    dispatchEvent: () => false,
  })) as unknown as typeof window.matchMedia;
}

// jsdom has no layout, so it ships no ResizeObserver either. The overflow cues
// that use one (the Members rail's bottom fade) ask a question jsdom can only
// answer "no" to, so the stub is a no-op: it exists to keep the component from
// throwing, not to simulate a resize.
if (!window.ResizeObserver) {
  window.ResizeObserver = class {
    observe() {}
    unobserve() {}
    disconnect() {}
  } as unknown as typeof ResizeObserver;
}

if (!HTMLImageElement.prototype.decode) {
  HTMLImageElement.prototype.decode = () => Promise.resolve();
}
