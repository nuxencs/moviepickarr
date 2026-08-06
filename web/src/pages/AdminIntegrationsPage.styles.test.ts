import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

const css = readFileSync(
  new URL("../components/moviepickarr/admin/integrations.css", import.meta.url),
  "utf8",
);
const appCss = readFileSync(new URL("../index.css", import.meta.url), "utf8");
const formSource = readFileSync(
  new URL("../components/moviepickarr/admin/TMDBSettingsForm.tsx", import.meta.url),
  "utf8",
);

function declarationsFrom(source: string, selector: string) {
  const escaped = selector.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  return source.match(new RegExp(`${escaped}\\s*\\{([^}]*)\\}`))?.[1] ?? "";
}

function declarations(selector: string) {
  return declarationsFrom(css, selector);
}

describe("Admin integrations workspace styles", () => {
  it("uses a compact three-column settings ledger", () => {
    expect(declarations(".int-setting")).toMatch(/display:\s*grid/);
    expect(declarations(".int-setting")).toMatch(
      /grid-template-columns:\s*minmax\(170px,\s*1fr\) 112px minmax\(220px,\s*280px\)/,
    );
    expect(declarations(".int-setting")).toMatch(/min-height:\s*62px/);
    expect(declarations(".int-setting__label")).toMatch(/display:\s*grid/);
    expect(declarations(".int-setting__control")).toMatch(/margin:\s*0/);
  });

  it("overlays help without changing a setting row's geometry", () => {
    expect(declarations(".int-help")).toMatch(/display:\s*inline-flex/);
    expect(formSource).toMatch(/className="iconbtn int-help__trigger"/);
    expect(declarationsFrom(appCss, ".iconbtn")).toMatch(/width:\s*34px/);
    expect(declarations(".int-help__trigger")).toMatch(/cursor:\s*help/);
    expect(declarations(".int-help__tooltip")).toMatch(/position:\s*fixed/);
    expect(declarations(".int-help__tooltip")).toMatch(
      /max-width:\s*calc\(100vw\s*-\s*16px\)/,
    );
    expect(declarations(".int-help__tooltip")).toMatch(/pointer-events:\s*none/);
    expect(declarations(".int-help__tooltip")).not.toMatch(/grid-column/);
    expect(declarations(".int-help__tooltip[hidden]")).toMatch(/display:\s*none/);
    expect(appCss).toMatch(
      /@media \(pointer: coarse\)[\s\S]*?\.iconbtn\s*\{[^}]*width:\s*40px;\s*height:\s*40px/,
    );
    expect(css).not.toMatch(/\.int-setting__meta/);
    expect(css).not.toMatch(/\.int-setting__head p/);
  });

  it("keeps the help hover target on the visible info control", () => {
    expect(declarations(".int-help")).toMatch(/justify-self:\s*start/);
  });

  it("highlights only the info icon on hover", () => {
    const hover = declarations(
      '.int-help .int-help__trigger:hover:not(:disabled):not([aria-disabled="true"])',
    );

    expect(hover).toMatch(/background:\s*transparent/);
    expect(hover).toMatch(/border-color:\s*transparent/);
    expect(hover).toMatch(/color:\s*var\(--ink\)/);
    expect(css).toMatch(
      /\.int-help\[data-open="true"\] \.int-help__trigger,\s*\.int-help\[data-open="true"\] \.int-help__trigger:hover:not\(:disabled\):not\(\[aria-disabled="true"\]\)\s*\{[^}]*color:\s*var\(--accent\)/,
    );
  });

  it("keeps the desktop status content at its existing top edge", () => {
    expect(css).toMatch(
      /@media \(min-width: 901px\)[\s\S]*?\.int-status\s*\{[^}]*padding-top:\s*6px/,
    );
  });

  it("collapses the ledger through the detail container, not the viewport", () => {
    expect(css).toMatch(
      /@container admin-integration \(max-width: 700px\)[\s\S]*?\.int-setting\s*\{[^}]*grid-template-columns:\s*minmax\(0,\s*1fr\) auto/,
    );
    expect(css).toMatch(
      /@container admin-integration \(max-width: 700px\)[\s\S]*?\.int-setting__value\s*\{[^}]*grid-column:\s*1\s*\/\s*-1/,
    );
    expect(css).toMatch(
      /@container admin-integration \(max-width: 700px\)[\s\S]*?\.int-status__facts\s*\{[^}]*grid-template-columns:\s*repeat\(2,\s*minmax\(0,\s*1fr\)\)/,
    );
  });

  it("contains no obsolete second-rail layout", () => {
    expect(css).not.toMatch(/\.admin-integrations__workspace/);
    expect(css).not.toMatch(/\.admin-integrations__rail/);
    expect(css).not.toMatch(/\.admin-integrations__detail/);
  });
});
