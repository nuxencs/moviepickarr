import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

const css = readFileSync(new URL("./integrations.css", import.meta.url), "utf8");

function declarations(selector: string) {
  const escaped = selector.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  return css.match(new RegExp(`${escaped}\\s*\\{([^}]*)\\}`))?.[1] ?? "";
}

describe("TMDB status styles", () => {
  it("uses a 980px measure and collapses active-run rows on phones", () => {
    expect(declarations(".int-status")).toMatch(/max-width:\s*980px/);
    expect(declarations(".int-status")).toMatch(/margin-inline:\s*auto/);
    expect(css).toMatch(
      /@container admin-integration \(max-width: 700px\)[\s\S]*?\.int-status__facts\s*\{[^}]*grid-template-columns:\s*repeat\(2,\s*minmax\(0,\s*1fr\)\)/,
    );
    expect(css).toMatch(
      /@media \(max-width: 520px\)[\s\S]*?\.int-status__facts\s*\{[^}]*grid-template-columns:\s*1fr/,
    );
    expect(css).toMatch(
      /@media \(max-width: 520px\)[\s\S]*?\.int-status__active\s*\{[^}]*grid-template-columns:\s*1fr/,
    );
  });
});
