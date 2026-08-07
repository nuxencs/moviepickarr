import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

const css = readFileSync(new URL("./radarr.css", import.meta.url), "utf8");

describe("Radarr Admin workspace styles", () => {
  it("uses flat registers for acquisition, setup, and webhook work", () => {
    expect(css).toMatch(/\.radarr-register\s*\{[^}]*border-top:\s*1px solid var\(--line\)/);
    expect(css).toMatch(/\.radarr-register__item\s*\{[^}]*border-bottom:\s*1px solid var\(--line\)/);
    expect(css).not.toMatch(/\.radarr-card/);
  });

  it("keeps acquisition rows usable at narrow container widths", () => {
    expect(css).toMatch(
      /@container admin-integration \(max-width: 760px\)[\s\S]*?\.radarr-acquisition-row\s*\{[^}]*grid-template-columns:\s*minmax\(0,\s*1fr\) 18px/,
    );
    expect(css).toMatch(
      /@media \(max-width: 640px\)[\s\S]*?\.radarr-detail__actions,[\s\S]*?align-items:\s*stretch/,
    );
  });

  it("honors reduced motion for the interactive register", () => {
    expect(css).toMatch(
      /@media \(prefers-reduced-motion: reduce\)[\s\S]*?\.radarr-acquisition-row[^}]*transition:\s*none/,
    );
  });
});
