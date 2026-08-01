import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

const css = readFileSync(new URL("../../index.css", import.meta.url), "utf8");
const heroStart = css.indexOf("/* ---- Hero ---- */");
const heroEnd = css.indexOf("/* ---- Draw reel", heroStart);
const heroCSS = css.slice(heroStart, heroEnd);

describe("the Hero layout contract", () => {
  it("keeps its body height reservation at mobile widths", () => {
    expect(heroCSS).toContain("min-height: var(--hero-body-h)");
    expect(heroCSS).not.toContain(".hero__body { min-height: 0; }");
    expect(heroCSS).toContain(".hero__body { align-self: stretch; }");
    expect(heroCSS).toContain("@media (max-width: 331px)");
    expect(heroCSS).toContain("--hero-body-h: 21rem");
  });
});
