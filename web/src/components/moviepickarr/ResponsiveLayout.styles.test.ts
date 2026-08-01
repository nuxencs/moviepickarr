import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

const css = readFileSync(new URL("../../index.css", import.meta.url), "utf8");

describe("the responsive layout contract", () => {
  it("grows real surfaces without scaling the document", () => {
    expect(css).toContain("--page-max: clamp(1240px, 80vw, 2560px)");
    expect(css).not.toMatch(/^\s*zoom\s*:/m);
    expect(css).not.toContain("--zoom:");
  });

  it("aligns the page, navigation, and hero to one width", () => {
    expect(css.match(/max-width: var\(--page-max\)/g)).toHaveLength(3);
  });

  it("keeps browse, record, and form dialogs on separate width tiers", () => {
    expect(css).toContain("--modal-browse-max: clamp(960px, 70vw, 1760px)");
    expect(css).toContain("--modal-record-max: clamp(880px, 52vw, 1320px)");
    expect(css).toContain("--modal-form-max: 460px");
    expect(css).toContain(".modal--movie { width: min(var(--modal-record-max), 100%);");
    expect(css).toContain(".modal--form { width: min(var(--modal-form-max), 100%); }");
  });

  it("gives movie records stable wide-screen geometry", () => {
    expect(css).toContain("--modal-record-block: clamp(900px, 84dvh, 1600px)");
    expect(css).toContain(".modal--movie.modal--capped {");
    expect(css).toContain("height: min(var(--modal-record-block), calc(100dvh - 96px));");
    expect(css).toContain("height: clamp(260px, 32cqi, 420px)");
    expect(css).toContain(".moviemodal__overview { max-width: 90ch;");
    expect(css).toContain(".moviemodal__hero { height: 190px;");
  });

  it("fills wrapping poster grids while keeping the pool more prominent", () => {
    expect(css).toContain("--poster-general: clamp(144px, 6.4vw, 164px)");
    expect(css).toContain(
      "grid-template-columns: repeat(auto-fill, minmax(164px, 1fr))",
    );
    expect(css).toContain(
      ".tile-grid--pool { grid-template-columns: repeat(auto-fill, minmax(184px, 1fr)); }",
    );
    expect(css).toContain("grid-template-columns: repeat(auto-fill, minmax(140px, 1fr))");
    expect(css).toContain("grid-template-columns: repeat(auto-fill, minmax(104px, 1fr))");
    expect(css).toContain("grid-template-columns: repeat(auto-fill, minmax(120px, 1fr))");
    expect(css).toContain("grid-template-columns: repeat(auto-fill, minmax(108px, 1fr))");
    expect(css).toContain("flex: 0 0 var(--poster-general)");
  });

  it("keeps ratings off posters and the wordmark still for pointer input", () => {
    expect(css).not.toContain(".poster__badge");
    expect(css).not.toContain(".wordmark__home:active");
    expect(css).not.toContain(".wordmark__home:hover .mark");
    expect(css).toContain(".wordmark__home:focus-visible");
  });
});
