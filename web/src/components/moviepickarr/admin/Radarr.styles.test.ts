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

  it("uses one animated disclosure and one aligned action inset", () => {
    expect(css).toMatch(
      /\.radarr-disclosure__viewport\s*\{[^}]*grid-template-rows:\s*0fr[^}]*transition:\s*grid-template-rows var\(--dur-base\)/,
    );
    expect(css).toMatch(
      /\.radarr-disclosure\[data-open="true"\][^{]*\.radarr-disclosure__viewport\s*\{[^}]*grid-template-rows:\s*1fr/,
    );
    expect(css).toMatch(
      /\.radarr-disclosure\[data-open="true"\][^{]*\.radarr-disclosure__chevron\s*\{[^}]*transform:\s*rotate\(90deg\)/,
    );
    expect(css).toMatch(/\.radarr-disclosure__trigger\s*\{[^}]*padding:\s*0;/);
    expect(css).toMatch(/\.radarr-current-action\s*\{[^}]*padding:\s*16px 0/);
  });

  it("keeps disclosures and the History filter visually flat", () => {
    expect(css).toMatch(
      /\.radarr-disclosure__trigger\s*\{[^}]*transition:\s*color var\(--dur-fast\)/,
    );
    expect(css).not.toMatch(
      /\.radarr-disclosure__trigger:hover,\s*\.radarr-disclosure__trigger:focus-visible\s*\{[^}]*background/,
    );
    expect(css).toMatch(
      /\.radarr-acquisition-history__tools \.field\s*\{[^}]*border-bottom:\s*1px solid var\(--line\)[^}]*background:\s*transparent/,
    );
  });

  it("honors reduced motion for the interactive register", () => {
    expect(css).toMatch(
      /@media \(prefers-reduced-motion: reduce\)[\s\S]*?\.radarr-acquisition-row[^}]*transition:\s*none/,
    );
    expect(css).toMatch(
      /@media \(prefers-reduced-motion: reduce\)[\s\S]*?\.radarr-disclosure__viewport,[\s\S]*?transition:\s*none/,
    );
  });
});
