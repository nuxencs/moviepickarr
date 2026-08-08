import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

const css = readFileSync(new URL("../../index.css", import.meta.url), "utf8");

function declarations(selector: string) {
  const escaped = selector.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  return css.match(new RegExp(`${escaped}\\s*\\{([^}]*)\\}`))?.[1] ?? "";
}

describe("shared control geometry", () => {
  it("keeps icon and text buttons on density tokens", () => {
    expect(declarations(":root")).toMatch(/--control-h-sm:\s*34px/);
    expect(declarations(":root")).toMatch(/--control-h:\s*42px/);
    expect(declarations(":root")).toMatch(/--control-h-lg:\s*46px/);
    expect(declarations(".btn")).toMatch(/height:\s*var\(--control-h\)/);
    expect(declarations(".btn--sm")).toMatch(/height:\s*var\(--control-h-sm\)/);
    expect(declarations(".iconbtn")).toMatch(/height:\s*var\(--control-h-sm\)/);
  });

  it("keeps labeled and icon-only glyphs fixed to their shared sizes", () => {
    expect(declarations(".btn svg")).toMatch(/width:\s*17px/);
    expect(declarations(".btn svg")).toMatch(/height:\s*17px/);
    expect(declarations(".btn svg")).toMatch(/flex:\s*none/);
    expect(declarations(".iconbtn svg")).toMatch(/width:\s*16px/);
    expect(declarations(".iconbtn svg")).toMatch(/height:\s*16px/);
    expect(declarations(".iconbtn svg")).toMatch(/flex:\s*none/);
  });

  it("gives every field type one visible wrapper focus treatment", () => {
    expect(declarations(".field")).toMatch(/height:\s*var\(--control-h\)/);
    expect(css).toMatch(/\.field:has\(:focus-visible\)\s*\{[^}]*outline:\s*2px solid var\(--accent\)/);
    expect(css).toMatch(/\.field textarea\s*\{[^}]*font:\s*14px var\(--font-ui\)/);
  });
});
