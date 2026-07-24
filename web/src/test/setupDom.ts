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

if (!HTMLImageElement.prototype.decode) {
  HTMLImageElement.prototype.decode = () => Promise.resolve();
}
