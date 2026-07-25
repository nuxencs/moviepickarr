/* ============================================================
   Render tests for the Modal shell, in both scroll modes (#177).

   The two modes differ only in CSS, and jsdom has no layout engine: it never
   sizes a box, so scrollHeight/clientHeight are 0 and getBoundingClientRect
   returns zeroes. Whether the veil's spacers survive a scroll to the end, or a
   capped surface centers, is measured in a real browser instead (the numbers
   are in the #177 commit). What jsdom CAN hold is everything around the mode:
   the surface has to carry `modal--capped` for those rules to select at all,
   plus the dialog behaviour the issue says must not regress in either mode.
   That's Esc, veil-click, the body-scroll lock, focus in and back out, and the
   deferred unmount that lets the exit motion play.

   Dismissal is driven the way a member does it (Escape, mousedown on the veil)
   and asserted through onClose plus what's on screen, not component state.
   ============================================================ */

import { act, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { Modal } from "@/components/moviepickarr/Modal";

/** Long enough to outrun exitDelayMs(), whatever the motion tokens say. */
const AFTER_EXIT = 1000;

function modalBody(close: () => void) {
  return (
    <div className="modal__scroll">
      <button type="button" onClick={close}>
        Close
      </button>
    </div>
  );
}

function renderModal(
  props: {
    capped?: boolean;
    dismissible?: boolean;
    open?: boolean;
    onRequestClose?: () => void;
  } = {},
) {
  const onClose = vi.fn();
  const view = render(
    <Modal onClose={onClose} className="modal--movie" {...props}>
      {modalBody}
    </Modal>,
  );
  return { onClose, view, dialog: screen.getByRole("dialog") };
}

/** The veil is the dialog's parent; a member clicks it to dismiss. */
function veilOf(dialog: HTMLElement) {
  return dialog.parentElement as HTMLElement;
}

function runExit() {
  act(() => void vi.advanceTimersByTime(AFTER_EXIT));
}

beforeEach(() => vi.useFakeTimers());
afterEach(() => vi.useRealTimers());

describe("Modal", () => {
  it("marks the surface capped only when asked, so the capped CSS can select it", () => {
    const { dialog } = renderModal({ capped: true });
    expect(dialog.classList.contains("modal--capped")).toBe(true);
    // The caller's own class rides along; capped is a mode, not a replacement.
    expect(dialog.classList.contains("modal--movie")).toBe(true);
  });

  it("leaves the surface uncapped by default, so short dialogs size to content", () => {
    const { dialog } = renderModal();
    expect(dialog.classList.contains("modal--capped")).toBe(false);
  });

  describe.each([
    ["uncapped", false],
    ["capped", true],
  ])("%s", (_name, capped) => {
    it("closes on Escape, after the exit motion has played", () => {
      const { onClose } = renderModal({ capped });

      fireEvent.keyDown(document, { key: "Escape" });
      // Still mounted: the surface stays put while its exit animation runs.
      expect(screen.queryByRole("dialog")).not.toBeNull();
      expect(onClose).not.toHaveBeenCalled();

      runExit();
      expect(onClose).toHaveBeenCalledTimes(1);
    });

    it("closes on a veil click but not on a click inside the surface", () => {
      const { onClose, dialog } = renderModal({ capped });

      fireEvent.mouseDown(dialog);
      runExit();
      expect(onClose).not.toHaveBeenCalled();

      fireEvent.mouseDown(veilOf(dialog));
      runExit();
      expect(onClose).toHaveBeenCalledTimes(1);
    });

    it("closes from the render-prop close", () => {
      const { onClose } = renderModal({ capped });

      fireEvent.click(screen.getByRole("button", { name: "Close" }));
      runExit();
      expect(onClose).toHaveBeenCalledTimes(1);
    });

    it("ignores Escape and veil clicks while pinned open", () => {
      const { onClose, dialog } = renderModal({ capped, dismissible: false });

      fireEvent.keyDown(document, { key: "Escape" });
      fireEvent.mouseDown(veilOf(dialog));
      runExit();

      expect(onClose).not.toHaveBeenCalled();
      expect(screen.queryByRole("dialog")).not.toBeNull();
    });

    it("locks body scroll while open and restores it on unmount", () => {
      document.body.style.overflow = "visible";
      const { view } = renderModal({ capped });

      expect(document.body.style.overflow).toBe("hidden");

      view.unmount();
      expect(document.body.style.overflow).toBe("visible");
    });

    it("takes focus on open and hands it back to the opener on unmount", () => {
      const opener = document.createElement("button");
      document.body.append(opener);
      opener.focus();

      const { view, dialog } = renderModal({ capped });
      expect(document.activeElement).toBe(dialog);

      view.unmount();
      expect(document.activeElement).toBe(opener);
      opener.remove();
    });
  });

  /* A history-backed modal (the movie modal, see #196) can't dismiss itself:
     the close it wants is a `back()`, and the exit motion has to run off the
     resulting state change rather than ahead of it. So the shell takes the
     parent's intent as `open` and hands every gesture to `onRequestClose`.
     Neither prop is passed by the local-state dialogs, whose behaviour is
     covered by the cases above. */
  describe("driven by the parent", () => {
    it("plays the exit when the parent withdraws open", () => {
      const onClose = vi.fn();
      const view = render(
        <Modal onClose={onClose} open>
          {modalBody}
        </Modal>,
      );

      view.rerender(
        <Modal onClose={onClose} open={false}>
          {modalBody}
        </Modal>,
      );
      // Same deal as a self-driven dismissal: on screen until the motion ends.
      expect(screen.queryByRole("dialog")).not.toBeNull();
      expect(onClose).not.toHaveBeenCalled();

      runExit();
      expect(onClose).toHaveBeenCalledTimes(1);
    });

    it.each([
      ["Escape", () => fireEvent.keyDown(document, { key: "Escape" })],
      ["a veil click", (dialog: HTMLElement) => fireEvent.mouseDown(veilOf(dialog))],
      ["the render-prop close", () => fireEvent.click(screen.getByRole("button", { name: "Close" }))],
    ])("asks the parent to close on %s instead of closing itself", (_name, gesture) => {
      const onRequestClose = vi.fn();
      const { onClose, dialog } = renderModal({ onRequestClose });

      gesture(dialog);
      runExit();

      expect(onRequestClose).toHaveBeenCalledTimes(1);
      // The parent owns the close; nothing happens until `open` comes back false.
      expect(onClose).not.toHaveBeenCalled();
      expect(screen.queryByRole("dialog")).not.toBeNull();
    });

    it("asks only once however many gestures land", () => {
      const onRequestClose = vi.fn();
      const { dialog } = renderModal({ onRequestClose });

      // Each request pops one history entry, so a double-Escape that asked
      // twice would pop the entry behind the modal and leave the page.
      fireEvent.keyDown(document, { key: "Escape" });
      fireEvent.keyDown(document, { key: "Escape" });
      fireEvent.mouseDown(veilOf(dialog));

      expect(onRequestClose).toHaveBeenCalledTimes(1);
    });

    it("stays put on a gesture while pinned open", () => {
      const onRequestClose = vi.fn();
      const { dialog } = renderModal({ onRequestClose, dismissible: false });

      fireEvent.keyDown(document, { key: "Escape" });
      fireEvent.mouseDown(veilOf(dialog));
      runExit();

      expect(onRequestClose).not.toHaveBeenCalled();
      expect(screen.queryByRole("dialog")).not.toBeNull();
    });
  });
});
