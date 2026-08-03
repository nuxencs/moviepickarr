import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

const css = readFileSync(new URL("../../index.css", import.meta.url), "utf8");
const heroStart = css.indexOf(".moviemodal__hero {");
const heroEnd = css.indexOf(".moviemodal__body {", heroStart);
const heroCSS = css.slice(heroStart, heroEnd);
const washRule = heroCSS.match(/\.moviemodal__hero__img--wash\s*\{([^}]*)\}/)?.[1] ?? "";

function customPx(name: string): number {
  const value = washRule.match(new RegExp(`${name}:\\s*(\\d+)px`))?.[1];
  expect(value).toBeDefined();
  return Number(value);
}

describe("the movie-modal hero contract", () => {
  // CSS blur samples transparency beyond its source. Keep the hero clip three
  // standard deviations inside that boundary, where its contribution is
  // negligible, and grow both axes symmetrically to put the source there.
  it("clips a wash whose source reaches beyond the blur kernel", () => {
    const blur = customPx("--wash-blur");
    const overscan = customPx("--wash-overscan");

    expect(heroCSS).toContain("overflow: hidden");
    expect(overscan).toBeGreaterThanOrEqual(blur * 3);
    expect(washRule).toContain("inset: calc(0px - var(--wash-overscan))");
    expect(washRule).toContain(
      "width: calc(100% + var(--wash-overscan) + var(--wash-overscan))",
    );
    expect(washRule).toContain("max-width: none");
    expect(washRule).toContain(
      "height: calc(100% + var(--wash-overscan) + var(--wash-overscan))",
    );
    expect(washRule).toContain("filter: blur(var(--wash-blur))");
  });
});
