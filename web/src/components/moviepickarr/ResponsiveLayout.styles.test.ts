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
    expect(css).toContain("--modal-record-max: clamp(880px, 58vw, 1560px)");
    expect(css).toContain("--modal-form-max: 460px");
    expect(css).toContain(".modal--movie { width: min(var(--modal-record-max), 100%); }");
    expect(css).toContain(".modal--form { width: min(var(--modal-form-max), 100%); }");
  });
});
