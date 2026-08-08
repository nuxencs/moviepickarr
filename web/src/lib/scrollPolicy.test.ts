// @vitest-environment jsdom

import { afterEach, describe, expect, it, vi } from "vitest";

import {
  documentOffsetTop,
  installDocumentScrollPolicy,
  lockPageScroll,
  resetDocumentScroll,
} from "@/lib/scrollPolicy";

afterEach(() => {
  document.documentElement.style.removeProperty("--document-scrollbar-width");
  document.body.removeAttribute("style");
  document.body.replaceChildren();
  vi.restoreAllMocks();
});

describe("document scroll policy", () => {
  it("publishes the measured native scrollbar width", () => {
    expect(installDocumentScrollPolicy()).toBe(0);
    expect(
      document.documentElement.style.getPropertyValue("--document-scrollbar-width"),
    ).toBe("0px");
  });

  it("measures offsets in the body owner's scroll coordinates", () => {
    const element = document.createElement("div");
    document.body.append(element);
    document.body.scrollTop = 500;
    vi.spyOn(document.body, "getBoundingClientRect").mockReturnValue({ top: 0 } as DOMRect);
    vi.spyOn(element, "getBoundingClientRect").mockReturnValue({ top: 120 } as DOMRect);

    expect(documentOffsetTop(element)).toBe(620);
  });

  it("resets the body owner instead of the window", () => {
    document.body.scrollTop = 5000;
    resetDocumentScroll();
    expect(document.body.scrollTop).toBe(0);
  });

  it("holds every page owner until the last nested lock releases", () => {
    document.body.style.overflow = "auto";
    const inner = document.createElement("div");
    inner.dataset.pageScrollOwner = "";
    inner.style.overflow = "scroll";
    document.body.append(inner);

    const releaseOuter = lockPageScroll();
    const releaseInner = lockPageScroll();
    expect(document.body.style.overflow).toBe("hidden");
    expect(inner.style.overflow).toBe("hidden");

    releaseOuter();
    expect(document.body.style.overflow).toBe("hidden");
    expect(inner.style.overflow).toBe("hidden");

    releaseInner();
    expect(document.body.style.overflow).toBe("auto");
    expect(inner.style.overflow).toBe("scroll");
  });
});
