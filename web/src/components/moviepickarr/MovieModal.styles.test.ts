import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

const css = readFileSync(new URL("../../index.css", import.meta.url), "utf8");
const heroStart = css.indexOf(".moviemodal__hero {");
const heroEnd = css.indexOf(".moviemodal__body {", heroStart);
const heroCSS = css.slice(heroStart, heroEnd);

describe("the movie-modal hero contract", () => {
  // The wash is scaled up so blur(48px) doesn't feather to transparent at the
  // box edges, which puts ~7.5% of the layer outside the hero. `.modal__scroll`
  // clips at the surface, not at the hero, so without this the overspill paints
  // down over the rail and the title. The two rules only work as a pair.
  it("clips the hero, because the wash layer is scaled past it", () => {
    expect(heroCSS).toContain("overflow: hidden");
    expect(heroCSS).toContain(".moviemodal__hero__img--wash { filter: blur(48px); transform: scale(1.15); }");
  });
});
