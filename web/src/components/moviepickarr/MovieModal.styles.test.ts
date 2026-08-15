import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

const css = readFileSync(new URL("../../index.css", import.meta.url), "utf8");
const scrollbarCSS = readFileSync(new URL("./movie-scrollbar.css", import.meta.url), "utf8");
const heroStart = css.indexOf(".moviemodal__hero {");
const heroEnd = css.indexOf(".moviemodal__body {", heroStart);
const heroCSS = css.slice(heroStart, heroEnd);
const scrollerRule = css.match(/\.moviemodal__scroll\s*\{[^}]*\}/)?.[0] ?? "";
const backdropRule = css.match(/\.moviemodal__backdrop\s*\{[^}]*\}/)?.[0] ?? "";

describe("the movie-modal hero contract", () => {
  it("spans decorative artwork beneath the custom scrollbar", () => {
    expect(heroCSS).toContain("overflow: hidden");
    expect(scrollerRule).toContain("position: relative");
    expect(scrollerRule).toContain("overflow-x: hidden");
    expect(backdropRule).toContain("position: absolute");
    expect(backdropRule).toContain("width: 100cqi");
    expect(backdropRule).not.toContain("z-index:");
    expect(heroCSS).not.toContain("isolation:");
    expect(heroCSS).not.toContain(".moviemodal__hero::after");
    expect(heroCSS).not.toMatch(/\.moviemodal__hero__img/);
  });

  it("uses inset custom rails with forgiving thumb targets", () => {
    expect(scrollbarCSS).toMatch(
      /\.modal--capped \.movie-scrollbar > \.moviemodal__scroll\s*\{[^}]*scrollbar-width:\s*none/,
    );
    expect(scrollbarCSS).toMatch(
      /\.movie-scrollbar > \.moviemodal__scroll::-webkit-scrollbar\s*\{[^}]*display:\s*none/,
    );
    expect(scrollbarCSS).toMatch(
      /\.movie-scrollbar__thumb-hit-area\s*\{[^}]*top:\s*-7px;[^}]*right:\s*0;[^}]*width:\s*12px/,
    );
    expect(scrollbarCSS).toMatch(
      /\.movie-cast-scrollbar__thumb-hit-area\s*\{[^}]*top:\s*1px;[^}]*left:\s*-8px;[^}]*height:\s*12px/,
    );
    expect(css).not.toContain(".moviemodal__scroll::-webkit-scrollbar");
  });
});
