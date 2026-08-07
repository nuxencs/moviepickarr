import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

const css = readFileSync(new URL("./admin-layout.css", import.meta.url), "utf8");

function declarations(selector: string, source = css) {
  const escaped = selector.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  return source.match(new RegExp(`${escaped}\\s*\\{([^}]*)\\}`))?.[1] ?? "";
}

describe("Admin navigation styles", () => {
  it("does not keep styles for the removed inner Admin heading", () => {
    expect(css).not.toMatch(/\.admin-layout__head/);
    expect(css).not.toMatch(/\.admin-section__back/);
  });

  it("uses one flat nested index beside the route content", () => {
    expect(declarations(".admin-layout")).toMatch(/display:\s*grid/);
    expect(declarations(".admin-layout")).toMatch(
      /grid-template-columns:\s*230px minmax\(0,\s*1fr\)/,
    );
    expect(declarations(".admin-layout__nav")).toMatch(
      /border-right:\s*1px solid var\(--line\)/,
    );
    expect(declarations(".admin-layout__nav")).toMatch(/background:\s*transparent/);
    expect(declarations(".admin-layout")).toMatch(/align-items:\s*stretch/);
    expect(declarations(".admin-layout__branch > .admin-layout__link")).toMatch(
      /padding-left:\s*11px/,
    );
    expect(declarations(".admin-layout__branch > .admin-layout__link")).toMatch(
      /appearance:\s*none/,
    );
    expect(declarations(".admin-layout__branch > .admin-layout__link")).toMatch(
      /border:\s*0/,
    );
    expect(declarations(".admin-layout__branch > .admin-layout__link")).toMatch(
      /width:\s*100%/,
    );
    expect(declarations(".admin-layout__branch > .admin-layout__link")).not.toMatch(
      /background:/,
    );
    expect(css).toMatch(
      /\.admin-layout__link:hover,\s*\.admin-layout__link:focus-visible\s*\{[^}]*background:\s*color-mix/,
    );
  });

  it("moves one persistent vertical indicator between the Admin destinations", () => {
    expect(declarations(".admin-layout__nav")).toMatch(/--admin-nav-row:\s*42px/);
    expect(declarations(".admin-layout__nav")).toMatch(
      /--admin-nav-step:\s*calc\(var\(--admin-nav-row\) \+ 2px\)/,
    );
    expect(declarations(".admin-layout__indicator")).toMatch(/position:\s*absolute/);
    expect(declarations(".admin-layout__indicator")).toMatch(
      /height:\s*var\(--admin-nav-row\)/,
    );
    expect(declarations(".admin-layout__indicator")).toMatch(
      /transition:\s*transform var\(--dur\) var\(--ease\)/,
    );
    expect(declarations('.admin-layout[data-section="roster"] .admin-layout__indicator')).toMatch(
      /transform:\s*translateY\(0\)/,
    );
    expect(declarations('.admin-layout[data-section="integrations"] .admin-layout__indicator')).toMatch(
      /transform:\s*translateY\(var\(--admin-nav-step\)\)/,
    );
    expect(declarations('.admin-layout[data-section="runs"] .admin-layout__indicator')).toMatch(
      /transform:\s*translateY\(calc\(var\(--admin-nav-step\) \+ var\(--admin-nav-step\)\)\)/,
    );
    expect(css).toMatch(
      /@media \(pointer: coarse\)[\s\S]*?\.admin-layout__nav\s*\{[^}]*--admin-nav-row:\s*48px/,
    );
    expect(declarations('.admin-layout__branch[data-active="true"]')).not.toMatch(/border-left/);
    expect(css).not.toMatch(/\.tab__ink/);
  });

  it("stretches the persistent indicator across the selected Integrations branch", () => {
    expect(declarations(".admin-layout__indicator")).toMatch(
      /transition:[^;]*transform var\(--dur\) var\(--ease\)[^;]*height var\(--dur\) var\(--ease\)/,
    );
    expect(
      declarations('.admin-layout[data-section="integrations"] .admin-layout__indicator'),
    ).toMatch(/height:\s*calc\(var\(--admin-nav-row\) \+ var\(--admin-nav-step\)\)/);
    expect(declarations(".admin-layout__indicator::before")).toMatch(
      /inset-block:\s*10px/,
    );
    expect(declarations(".admin-layout__indicator::before")).not.toMatch(
      /height:\s*22px/,
    );
  });

  it("reveals the integration child without snapping the following destination", () => {
    expect(declarations(".admin-layout__children")).toMatch(/grid-template-rows:\s*0fr/);
    expect(declarations(".admin-layout__children")).toMatch(/opacity:\s*0/);
    expect(declarations(".admin-layout__children")).toMatch(
      /transition:[^;]*grid-template-rows var\(--dur\) var\(--ease\)[^;]*opacity var\(--dur-fast\) var\(--ease\)/,
    );
    expect(declarations('.admin-layout__children[data-open="true"]')).toMatch(
      /grid-template-rows:\s*1fr/,
    );
    expect(declarations('.admin-layout__children[data-open="true"]')).toMatch(/opacity:\s*1/);
    expect(declarations(".admin-layout__children-inner")).toMatch(/min-height:\s*0/);
    expect(declarations(".admin-layout__children-inner")).toMatch(/overflow:\s*hidden/);
  });

  it("keeps the integration child slot on the indicator's shared row step", () => {
    expect(declarations(".admin-layout__nav")).toMatch(
      /--admin-nav-child-tail:\s*8px/,
    );
    expect(declarations(".admin-layout__integration")).toMatch(
      /min-height:\s*calc\(var\(--admin-nav-step\) - var\(--admin-nav-child-tail\)\)/,
    );
    expect(declarations(".admin-layout__integration")).toMatch(
      /margin:\s*0 9px var\(--admin-nav-child-tail\) 35px/,
    );
    expect(css).toMatch(
      /@media \(pointer: coarse\)[\s\S]*?\.admin-layout__nav\s*\{[^}]*--admin-nav-child-tail:\s*2px/,
    );
    expect(declarations(".admin-layout__children-inner")).not.toMatch(
      /(?:^|;)\s*height:/,
    );
    expect(
      declarations(
        '.admin-layout__children[data-open="true"] .admin-layout__children-inner',
      ),
    ).not.toMatch(/(?:^|;)\s*height:/);
  });

  it("uses a flat text cue for the selected integration", () => {
    const selected = declarations('.admin-layout__integration[aria-current="page"]');

    expect(selected).toMatch(/background:\s*transparent/);
    expect(selected).toMatch(
      /color:\s*color-mix\(in oklab,\s*var\(--accent\) [^,]+,\s*var\(--ink\)\)/,
    );
    expect(selected).toMatch(/font-weight:\s*650/);
    expect(css).not.toMatch(/\.admin-layout__integration-marker/);
  });

  it("marks the current mobile leaf with a horizontal accent", () => {
    expect(css).toMatch(
      /@media not all and \(min-width: 901px\)[\s\S]*?\.admin-layout__link\[aria-current="page"\],\s*\.admin-layout__integration\[aria-current="page"\]\s*\{[^}]*box-shadow:\s*inset 0 -2px var\(--accent\)[^}]*color:\s*var\(--accent\)/,
    );
  });

  it("gives every desktop Admin destination the same internal scroll boundary", () => {
    expect(css).toMatch(
      /@media \(min-width: 901px\)[\s\S]*?\.shell--admin\s*\{[^}]*height:\s*calc\(100dvh - 63px\)[^}]*overflow:\s*hidden[^}]*padding-bottom:\s*30px/,
    );
    expect(css).toMatch(
      /@media \(min-width: 901px\)[\s\S]*?\.admin-layout > \.admin-layout__body\s*\{[^}]*overflow-x:\s*clip[^}]*overflow-y:\s*auto[^}]*overscroll-behavior-y:\s*contain[^}]*scrollbar-gutter:\s*stable/,
    );
    expect(css).not.toMatch(/\.shell:has\(\.admin-layout/);
  });

  it("keeps the persistent content inset stable while routes swap", () => {
    expect(declarations(".admin-layout__body")).toMatch(
      /padding:\s*16px clamp\(22px,\s*3vw,\s*46px\) 0/,
    );
    expect(css).not.toMatch(
      /\.admin-layout\[data-section="integrations"\]\s*>\s*\.admin-layout__body\s*\{/,
    );
  });

  it("partitions fractional widths into desktop or mobile layout without a gap", () => {
    expect(css).toMatch(
      /@media not all and \(min-width: 901px\)[\s\S]*?\.admin-layout\s*\{[^}]*grid-template-columns:\s*minmax\(0,\s*1fr\)/,
    );
    expect(css).toMatch(
      /@media not all and \(min-width: 901px\)[\s\S]*?\.admin-layout__nav\s*\{[^}]*overflow-x:\s*auto[^}]*overflow-y:\s*hidden[^}]*border-right:\s*0[^}]*border-bottom:\s*1px solid var\(--line\)/,
    );
    expect(css).not.toMatch(/@media \(max-width: 900px\)/);
    const mobile = css.match(
      /@media not all and \(min-width: 901px\)\s*\{([\s\S]*?)\n\}/,
    )?.[1] ?? "";
    expect(mobile).not.toMatch(/\.admin-layout__body[^}]*overflow-y:\s*(?:auto|scroll)/);
  });
});
