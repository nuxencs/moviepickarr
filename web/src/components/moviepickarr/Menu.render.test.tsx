/* Render test for the open Menu's scroll repositioning: one layout-forcing
   pass per frame. It counts rect reads, against the usual rule (see
   vitest.config.ts), because throttled and unthrottled land in the same place. */

import { act, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { Menu } from "@/components/moviepickarr/Menu";

let frames: FrameRequestCallback[] = [];
let rectSpy: ReturnType<typeof vi.spyOn>;

/** Run every frame callback queued so far (one compositor tick). */
function flushFrame() {
  const queued = frames;
  frames = [];
  act(() => {
    for (const cb of queued) cb(0);
  });
}

beforeEach(() => {
  frames = [];
  vi.stubGlobal("requestAnimationFrame", (cb: FrameRequestCallback) => frames.push(cb));
  vi.stubGlobal("cancelAnimationFrame", () => {});
  rectSpy = vi.spyOn(Element.prototype, "getBoundingClientRect");
});

afterEach(() => {
  vi.unstubAllGlobals();
  rectSpy.mockRestore();
});

function openMenu() {
  render(<Menu label="More actions" actions={[{ label: "Edit", onSelect: () => {} }]} />);
  act(() => screen.getByRole("button", { name: "More actions" }).click());
  // The mount pass positions the menu; only what scrolling adds matters below.
  flushFrame();
  rectSpy.mockClear();
}

describe("Menu", () => {
  it("coalesces a burst of scroll ticks into one reposition per frame", () => {
    openMenu();

    act(() => {
      for (let i = 0; i < 5; i++) window.dispatchEvent(new Event("scroll"));
    });
    // Nothing measured yet: the burst waits on a single frame request.
    expect(rectSpy).not.toHaveBeenCalled();

    flushFrame();
    // One pass: the trigger rect and the menu rect.
    expect(rectSpy).toHaveBeenCalledTimes(2);
  });

  it("measures again on the next frame after a later scroll", () => {
    openMenu();

    act(() => window.dispatchEvent(new Event("scroll")));
    flushFrame();
    expect(rectSpy).toHaveBeenCalledTimes(2);

    act(() => window.dispatchEvent(new Event("scroll")));
    flushFrame();
    expect(rectSpy).toHaveBeenCalledTimes(4);
  });

  it("restores focus inside when a live action transition removes the focused item", () => {
    const { rerender } = render(
      <Menu
        label="More actions"
        actions={[
          { label: "Replace link", onSelect: () => {} },
          { label: "Revoke link", onSelect: () => {} },
        ]}
      />,
    );
    act(() => screen.getByRole("button", { name: "More actions" }).click());
    act(() => screen.getByRole("menuitem", { name: "Revoke link" }).focus());

    rerender(
      <Menu
        label="More actions"
        actions={[
          { label: "Create new link", onSelect: () => {} },
          { label: "Dismiss link", onSelect: () => {} },
        ]}
      />,
    );

    expect(document.activeElement).toBe(screen.getByRole("menuitem", { name: "Create new link" }));
  });

  it("stays focusable but does not open when every action is disabled", () => {
    render(
      <Menu
        label="More actions"
        actions={[
          { label: "Replace link", disabled: true, onSelect: () => {} },
          { label: "Remove member", disabled: true, onSelect: () => {} },
        ]}
      />,
    );

    const trigger = screen.getByRole("button", { name: "More actions" }) as HTMLButtonElement;
    expect(trigger.disabled).toBe(false);
    expect(trigger.getAttribute("aria-disabled")).toBe("true");
    act(() => trigger.focus());
    act(() => trigger.click());
    expect(document.activeElement).toBe(trigger);
    expect(screen.queryByRole("menu")).toBeNull();
  });

  it("returns focus to a busy trigger and prevents it from reopening", () => {
    const { rerender } = render(
      <Menu
        label="More actions"
        actions={[{ label: "Replace link", onSelect: () => {} }]}
      />,
    );
    const trigger = screen.getByRole("button", { name: "More actions" });
    act(() => trigger.click());
    act(() => screen.getByRole("menuitem", { name: "Replace link" }).focus());

    rerender(
      <Menu
        label="More actions"
        actions={[{ label: "Replace link", onSelect: () => {} }]}
        disabled
      />,
    );

    expect(document.activeElement).toBe(trigger);
    expect(trigger.getAttribute("aria-disabled")).toBe("true");
    act(() => trigger.click());
    expect(document.activeElement).toBe(trigger);
  });
});
