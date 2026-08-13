import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

const css = readFileSync(new URL("../../index.css", import.meta.url), "utf8");
const heroStart = css.indexOf(".moviemodal__hero {");
const heroEnd = css.indexOf(".moviemodal__body {", heroStart);
const heroCSS = css.slice(heroStart, heroEnd);
const scrollerRule = css.match(/\.moviemodal__scroll\s*\{[^}]*\}/)?.[0] ?? "";
const backdropRule = css.match(/\.moviemodal__backdrop\s*\{[^}]*\}/)?.[0] ?? "";

describe("the movie-modal hero contract", () => {
  it("overscans decorative artwork beneath the native scrollbar", () => {
    expect(heroCSS).toContain("overflow: hidden");
    expect(scrollerRule).toContain("position: relative");
    expect(scrollerRule).toContain("overflow-x: hidden");
    expect(backdropRule).toContain("position: absolute");
    expect(backdropRule).toContain("width: calc(100% + var(--document-scrollbar-width))");
    expect(backdropRule).not.toContain("z-index:");
    expect(heroCSS).not.toContain("isolation:");
    expect(heroCSS).not.toContain(".moviemodal__hero::after");
    expect(heroCSS).not.toMatch(/\.moviemodal__hero__img/);
  });
});
