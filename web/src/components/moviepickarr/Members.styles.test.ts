import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

const pushQuery = "not all and (min-width: 761px)";
const membersCSS = readFileSync(new URL("./members.css", import.meta.url), "utf8");
const desktopBlock = membersCSS.slice(
  membersCSS.indexOf("@media (min-width: 761px)"),
  membersCSS.indexOf("@media (max-width: 900px)"),
);
const block = membersCSS.slice(membersCSS.indexOf("/* ---- the push, below 761"));
const at = block.indexOf(`@media ${pushQuery}`);
const aboveQuery = block.slice(0, at);
const insideQuery = block.slice(at, block.indexOf("/* ---- the loading skeleton"));

describe("the Members mobile push styles", () => {
  it("uses the baseline-compatible exact width complement", () => {
    expect(membersCSS).toContain(`@media ${pushQuery}`);
  });

  it("keeps both mobile screens on one fixed grid origin", () => {
    expect(membersCSS).toContain('grid-template-areas: "screen"');
    expect(membersCSS).toMatch(
      /\.mem-rail-screen,\s*\.mem-pane\s*{\s*grid-area: screen;/,
    );
    expect(membersCSS).toContain(".mem-rail-screen > .sec-head");
  });

  it("flattens the rail screen back into the desktop board grid", () => {
    expect(desktopBlock).toMatch(/grid-template-areas:\s*"head head"\s*"rail pane";/);
    expect(desktopBlock).toMatch(/\.mem-rail-screen\s*{\s*display: contents;/);
    expect(desktopBlock).toMatch(/\.mem-rail-screen > \.sec-head\s*{\s*grid-area: head;/);
    expect(desktopBlock).toMatch(/\.mem-rail-screen > \.mem-rail\s*{\s*grid-area: rail;/);
    expect(desktopBlock).toMatch(/\.mem-pane\s*{\s*grid-area: pane;/);
  });

  it("compensates only a shell that owns the section head", () => {
    expect(membersCSS).toMatch(/\.mem__shell\s*{\s*--mem-head-space: 0px;/);
    expect(membersCSS).toMatch(
      /\.mem__shell--with-head\s*{[\s\S]*?--mem-head-space: 52px;/,
    );
    expect(desktopBlock).toContain("+ var(--mem-head-space)");
  });

  it("animates one number declared above the width query", () => {
    // Registered, so it interpolates rather than jumping. Declaring it where
    // the width query cannot reach it keeps every duration tied to the URL.
    expect(aboveQuery).toContain("@property --mem-in");
    expect(aboveQuery).toMatch(/--mem-in:\s*0;/);
    expect(aboveQuery).toContain("transition:");
    expect(insideQuery).not.toMatch(/^\s*transition(-property)?:/m);
  });

  it("reads both transforms and opacity from that number", () => {
    expect(insideQuery).toContain("opacity: var(--mem-in)");
    expect(insideQuery).toMatch(
      /\.mem-rail-screen \{\s*transform: translateX\(calc\(/,
    );
    expect(insideQuery).toMatch(/\.mem-pane \{\s*transform: translateX\(calc\(/);
  });

  it("holds display none until the exit ends", () => {
    expect(aboveQuery).toContain("allow-discrete");
    expect(insideQuery).toMatch(/position: absolute;\s*inset-inline: 0;/);
    expect(insideQuery).toContain("display: none");
  });

  it("uses a hard cut under reduced motion", () => {
    expect(aboveQuery).toContain("@media (prefers-reduced-motion: reduce)");
  });
});
