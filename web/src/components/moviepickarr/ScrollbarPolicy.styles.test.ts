import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

const css = readFileSync(new URL("../../index.css", import.meta.url), "utf8");
const adminCSS = readFileSync(
  new URL("./admin/admin-layout.css", import.meta.url),
  "utf8",
);
const membersCSS = readFileSync(new URL("./members.css", import.meta.url), "utf8");
const authCSS = readFileSync(new URL("./auth/auth.css", import.meta.url), "utf8");

describe("the native scrollbar policy", () => {
  it("uses the body as the stable document owner", () => {
    expect(css).toMatch(/html\s*\{[^}]*height:\s*100%[^}]*overflow:\s*hidden/);
    expect(css).toMatch(
      /body\s*\{[^}]*height:\s*100dvh[^}]*overflow-x:\s*clip[^}]*overflow-y:\s*auto[^}]*scrollbar-gutter:\s*stable/,
    );
  });

  it("keeps shared navigation in the document frame across bounded owners", () => {
    const navRule = css.match(/\.nav\s*\{[^}]*\}/)?.[0] ?? "";
    const bottomNavRule = css.match(/\.navbar-bottom\s*\{[^}]*\}/)?.[0] ?? "";

    expect(css).toContain("--document-frame-offset: var(--document-scrollbar-width)");
    expect(navRule).toContain("margin-inline-end: var(--document-frame-offset)");
    expect(bottomNavRule).toContain("right: var(--document-scrollbar-width)");
  });

  it("leaves native WebKit scrollbars unstyled", () => {
    expect(css).not.toContain("::-webkit-scrollbar");
  });

  it("reserves geometry on visible bounded owners", () => {
    expect(css).toMatch(/\.modal-veil\s*\{[^}]*scrollbar-gutter:\s*stable/);
    expect(css).toMatch(/\.modal--capped \.modal__scroll\s*\{[^}]*scrollbar-gutter:\s*stable/);
    expect(css).toMatch(/\.filtermenu__list\s*\{[^}]*scrollbar-gutter:\s*stable/);
    expect(adminCSS).toMatch(/\.admin-layout > \.admin-layout__nav\s*\{[^}]*scrollbar-gutter:\s*stable/);
    expect(membersCSS).toMatch(/\.mem-pane\s*\{[^}]*scrollbar-gutter:\s*stable/);
    expect(authCSS).toMatch(/\.auth\s*\{[^}]*scrollbar-gutter:\s*stable/);
  });

  it("keeps movie-modal chrome in the native scrollbar paint plane", () => {
    const heroRule = css.match(/\.moviemodal__hero\s*\{[^}]*\}/)?.[0] ?? "";
    const heroScrimRule = css.match(/\.moviemodal__hero::after\s*\{[^}]*\}/)?.[0] ?? "";
    const heroShimmerRule = css.match(/\.moviemodal__hero__shimmer\s*\{[^}]*\}/)?.[0] ?? "";
    const movieVeilRule = css.match(/\.modal-veil:has\(\.modal--movie\)\s*\{[^}]*\}/)?.[0] ?? "";
    const movieRule = css.match(/\.modal--movie\s*\{[^}]*\}/)?.[0] ?? "";

    expect(heroRule).not.toContain("isolation:");
    expect(heroScrimRule).not.toContain("z-index:");
    expect(heroShimmerRule).not.toContain("z-index:");
    expect(css).not.toMatch(/\.moviemodal__hero__img\s*\{/);
    expect(movieVeilRule).toContain("backdrop-filter: none");
    expect(movieRule).toContain("animation: mg-fadeIn");
  });

  it("keeps navigation in the body paint plane", () => {
    const navRule = css.match(/\.nav\s*\{[^}]*\}/)?.[0] ?? "";
    const bottomNavRule = css.match(/\.navbar-bottom\s*\{[^}]*\}/)?.[0] ?? "";

    expect(navRule).toContain("position: sticky");
    expect(navRule).not.toContain("backdrop-filter:");
    expect(bottomNavRule).not.toContain("backdrop-filter:");
  });
});
