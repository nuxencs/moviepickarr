import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

const css = readFileSync(new URL("../../index.css", import.meta.url), "utf8");
const heroStart = css.indexOf(".moviemodal__hero {");
const heroEnd = css.indexOf(".moviemodal__body {", heroStart);
const heroCSS = css.slice(heroStart, heroEnd);
const scrollerRule = css.match(/\.moviemodal__scroll\s*\{[^}]*\}/)?.[0] ?? "";

describe("the movie-modal hero contract", () => {
  it("paints the photograph on the scroll owner instead of a competing image layer", () => {
    expect(heroCSS).toContain("overflow: hidden");
    expect(scrollerRule).toContain("background-attachment: local");
    expect(heroCSS).not.toContain("isolation:");
    expect(heroCSS).not.toContain(".moviemodal__hero::after");
    expect(heroCSS).not.toMatch(/\.moviemodal__hero__img/);
  });
});
