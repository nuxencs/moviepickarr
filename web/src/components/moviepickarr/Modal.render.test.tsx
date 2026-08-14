/* ============================================================
   Render tests for the Modal shell, in both scroll modes (#177).

   The two modes differ only in CSS, and jsdom has no layout engine: it never
   sizes a box, so scrollHeight/clientHeight are 0 and getBoundingClientRect
   returns zeroes. Whether the veil's spacers survive a scroll to the end, or a
   capped surface centers, is measured in a real browser instead (the numbers
   are in the #177 commit). What jsdom CAN hold is everything around the mode:
   the surface has to carry `modal--capped` for those rules to select at all,
   plus the dialog behaviour the issue says must not regress in either mode.
   That's Esc, veil-click, the page-owner scroll lock, focus in and back out, and the
   deferred unmount that lets the exit motion play.

   Dismissal is driven the way a member does it (Escape, a press and release on
   the veil)
   and asserted through onClose plus what's on screen, not component state.
   ============================================================ */

import { act, fireEvent, render, screen } from "@testing-library/react";
import { useState } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { Menu } from "@/components/moviepickarr/Menu";
import { Modal } from "@/components/moviepickarr/Modal";

/** Long enough to outrun exitDelayMs(), whatever the motion tokens say. */
const AFTER_EXIT = 1000;

function modalBody(close: () => void) {
  return (
    <div className="modal__scroll">
      <h2>Test modal</h2>
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
    <Modal label="Test modal" onClose={onClose} className="modal--movie" {...props}>
      {modalBody}
    </Modal>,
  );
  return { onClose, view, dialog: screen.getByRole("dialog") };
}

/** The veil is the dialog's parent; a member clicks it to dismiss. */
function veilOf(dialog: HTMLElement) {
  return dialog.parentElement as HTMLElement;
}

/** Dismissal runs on the release, so a veil click is both halves of it (#310). */
function clickVeil(dialog: HTMLElement) {
  const veil = veilOf(dialog);
  fireEvent.mouseDown(veil);
  fireEvent.mouseUp(veil);
}

function runExit() {
  act(() => void vi.advanceTimersByTime(AFTER_EXIT));
}

beforeEach(() => vi.useFakeTimers());
afterEach(() => {
  vi.useRealTimers();
  vi.restoreAllMocks();
});

describe("Modal", () => {
  it("exposes its visible title as the dialog name", () => {
    renderModal();

    expect(screen.getByRole("dialog", { name: "Test modal" })).not.toBeNull();
  });

  it("keeps the visual backdrop beside the dialog scroll tree", () => {
    const { dialog } = renderModal();
    const backdrop = dialog.previousElementSibling;

    expect(backdrop?.classList.contains("modal-backdrop")).toBe(true);
    expect(backdrop?.getAttribute("aria-hidden")).toBe("true");
    expect(backdrop?.parentElement).toBe(dialog.parentElement);
    expect(backdrop?.contains(dialog)).toBe(false);
  });

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

    it("closes only after a veil press is released on the veil", () => {
      const { onClose, dialog } = renderModal({ capped });
      const veil = veilOf(dialog);

      // Veil -> surface: the valid press cancels the browser default, but a
      // mismatched release is inert.
      expect(fireEvent.mouseDown(veil)).toBe(false);
      expect(dialog.classList.contains("modal--closing")).toBe(false);
      fireEvent.mouseUp(dialog);
      expect(dialog.classList.contains("modal--closing")).toBe(false);

      // Surface -> veil: the surface press keeps its default and does not arm
      // the veil's dismissal.
      expect(fireEvent.mouseDown(dialog)).toBe(true);
      fireEvent.mouseUp(veil);
      expect(dialog.classList.contains("modal--closing")).toBe(false);

      // Matching halves leave the surface open on the press, then start its
      // exit on the release.
      expect(fireEvent.mouseDown(veil)).toBe(false);
      expect(dialog.classList.contains("modal--closing")).toBe(false);
      fireEvent.mouseUp(veil);
      expect(dialog.classList.contains("modal--closing")).toBe(true);

      runExit();
      expect(onClose).toHaveBeenCalledTimes(1);
    });

    it("leaves later page presses alone", () => {
      const { dialog } = renderModal({ capped });
      const behind = document.createElement("p");
      behind.textContent = "text under the veil";
      document.body.appendChild(behind);

      clickVeil(dialog);

      // The veil press already canceled the selection default that belonged to
      // the dismissal. A later page press is the page's to handle.
      expect(fireEvent.mouseDown(behind, { detail: 2 })).toBe(true);

      behind.remove();
    });

    it("does not close from a click inside the surface", () => {
      const { onClose, dialog } = renderModal({ capped });

      fireEvent.mouseDown(dialog);
      fireEvent.mouseUp(dialog);
      runExit();
      expect(onClose).not.toHaveBeenCalled();
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
      clickVeil(dialog);
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

    it("locks marked inner page owners without adding padding compensation", () => {
      document.body.style.paddingRight = "7px";
      const inner = document.createElement("div");
      inner.dataset.pageScrollOwner = "";
      inner.style.overflow = "auto";
      document.body.append(inner);

      const { view } = renderModal({ capped });
      expect(inner.style.overflow).toBe("hidden");
      expect(document.body.style.paddingRight).toBe("7px");

      view.unmount();
      expect(inner.style.overflow).toBe("auto");
      expect(document.body.style.paddingRight).toBe("7px");
      inner.remove();
      document.body.style.paddingRight = "";
    });

    it("takes focus on open and hands it back to the opener on unmount", () => {
      const opener = document.createElement("button");
      document.body.append(opener);
      opener.focus();
      const restoreFocus = vi.spyOn(opener, "focus");

      const { view, dialog } = renderModal({ capped });
      expect(document.activeElement).toBe(dialog);

      view.unmount();
      expect(document.activeElement).toBe(opener);
      expect(restoreFocus).toHaveBeenCalledWith({ preventScroll: true });
      opener.remove();
    });

    it("takes focus without asking the browser to scroll the page owner", () => {
      const focus = vi.spyOn(HTMLElement.prototype, "focus");

      const { dialog } = renderModal({ capped });

      expect(document.activeElement).toBe(dialog);
      expect(focus).toHaveBeenCalledWith({ preventScroll: true });
    });
  });

  it("restores focus inside the surviving opener region when the opener is removed", () => {
    function DetachedOpener() {
      const [open, setOpen] = useState(false);
      const [showOpener, setShowOpener] = useState(true);

      return (
        <section>
          {showOpener && (
            <button type="button" onClick={() => setOpen(true)}>
              Open movie
            </button>
          )}
          <button type="button" tabIndex={-1}>
            Non-tabbable movie action
          </button>
          <button type="button">Next movie</button>
          {open && (
            <Modal label="Movie details" onClose={() => setOpen(false)}>
              {(close) => (
                <>
                  <h2>Movie details</h2>
                  <button type="button" onClick={() => setShowOpener(false)}>
                    Remove opening movie
                  </button>
                  <button type="button" onClick={close}>
                    Close
                  </button>
                </>
              )}
            </Modal>
          )}
        </section>
      );
    }

    render(<DetachedOpener />);
    const opener = screen.getByRole("button", { name: "Open movie" });
    opener.focus();
    fireEvent.click(opener);
    fireEvent.click(screen.getByRole("button", { name: "Remove opening movie" }));
    fireEvent.click(screen.getByRole("button", { name: "Close" }));
    runExit();

    expect(document.activeElement).toBe(screen.getByRole("button", { name: "Next movie" }));
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
        <Modal label="Test modal" onClose={onClose} open>
          {modalBody}
        </Modal>,
      );

      view.rerender(
        <Modal label="Test modal" onClose={onClose} open={false}>
          {modalBody}
        </Modal>,
      );
      // Same deal as a self-driven dismissal: on screen until the motion ends.
      expect(screen.queryByRole("dialog")).not.toBeNull();
      expect(onClose).not.toHaveBeenCalled();

      runExit();
      expect(onClose).toHaveBeenCalledTimes(1);
    });

    it("cancels a parent-driven exit when open returns before it finishes", () => {
      const onClose = vi.fn();
      const onRequestClose = vi.fn();
      const view = render(
        <Modal label="Test modal" onClose={onClose} onRequestClose={onRequestClose} open>
          {modalBody}
        </Modal>,
      );
      const dialog = screen.getByRole("dialog");
      const closeButton = screen.getByRole("button", { name: "Close" });
      closeButton.focus();

      // A gesture owns one request for this open interval. The parent then
      // withdraws its history-backed state and restores it before the exit lands.
      fireEvent.keyDown(document, { key: "Escape" });
      expect(onRequestClose).toHaveBeenCalledTimes(1);
      view.rerender(
        <Modal label="Test modal" onClose={onClose} onRequestClose={onRequestClose} open={false}>
          {modalBody}
        </Modal>,
      );
      expect(dialog.classList.contains("modal--closing")).toBe(true);

      view.rerender(
        <Modal label="Test modal" onClose={onClose} onRequestClose={onRequestClose} open>
          {modalBody}
        </Modal>,
      );
      expect(dialog.classList.contains("modal--closing")).toBe(false);

      // Reopening keeps this exact surface and its focus alive, cancels the old
      // timer, and gives the restored open interval one fresh close request.
      runExit();
      expect(onClose).not.toHaveBeenCalled();
      expect(screen.getByRole("dialog")).toBe(dialog);
      expect(document.activeElement).toBe(closeButton);
      fireEvent.keyDown(document, { key: "Escape" });
      expect(onRequestClose).toHaveBeenCalledTimes(2);
    });

    it.each([
      ["Escape", () => fireEvent.keyDown(document, { key: "Escape" })],
      ["a veil click", (dialog: HTMLElement) => clickVeil(dialog)],
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
      clickVeil(dialog);

      expect(onRequestClose).toHaveBeenCalledTimes(1);
    });

    it("stays put on a gesture while pinned open", () => {
      const onRequestClose = vi.fn();
      const { dialog } = renderModal({ onRequestClose, dismissible: false });

      fireEvent.keyDown(document, { key: "Escape" });
      clickVeil(dialog);
      runExit();

      expect(onRequestClose).not.toHaveBeenCalled();
      expect(screen.queryByRole("dialog")).not.toBeNull();
    });
  });

  /* A confirm opened from inside a dialog (#220): both surfaces portal into
     <body> as siblings, so nothing about the DOM tells the outer one that
     something is on top of it. Escape, the veil and the Tab trap all have to
     stop at the topmost surface, or one press takes the whole stack down. */
  describe("opened from inside another modal", () => {
    /** Outer dialog with its own field; the inner one mounts on demand. */
    function renderNested() {
      const onOuterClose = vi.fn();
      const onInnerClose = vi.fn();
      let hideOpener = () => {};

      function Nested() {
        const [outer, setOuter] = useState(true);
        const [inner, setInner] = useState(false);
        const [showOpener, setShowOpener] = useState(true);
        hideOpener = () => setShowOpener(false);
        if (!outer) return null;
        return (
          <Modal
            label="Outer modal"
            onClose={() => {
              setOuter(false);
              onOuterClose();
            }}
          >
            {(close) => (
              <>
                {showOpener && (
                  <button type="button" onClick={() => setInner(true)}>
                    Delete
                  </button>
                )}
                <button type="button" onClick={close}>
                  Close outer
                </button>
                {inner && (
                  <Modal
                    label="Inner modal"
                    onClose={() => {
                      setInner(false);
                      onInnerClose();
                    }}
                  >
                    {(closeInner) => (
                      <button type="button" onClick={closeInner}>
                        Cancel
                      </button>
                    )}
                  </Modal>
                )}
              </>
            )}
          </Modal>
        );
      }

      const view = render(<Nested />);
      const opener = screen.getByRole("button", { name: "Delete" });
      opener.focus();
      act(() => void fireEvent.click(opener));
      const [outerDialog, innerDialog] = screen.getAllByRole("dialog", { hidden: true });
      return {
        onOuterClose,
        onInnerClose,
        outer: outerDialog,
        inner: innerDialog,
        hideOpener,
        view,
      };
    }

    it("exposes only the top dialog, then restores the outer one", () => {
      const { outer, inner } = renderNested();
      const opener = outer.querySelector("button") as HTMLButtonElement;

      expect(screen.getAllByRole("dialog")).toEqual([inner]);
      expect(outer.getAttribute("aria-modal")).toBeNull();
      expect(outer.getAttribute("aria-hidden")).toBe("true");
      expect(outer.hasAttribute("inert")).toBe(true);
      expect(inner.getAttribute("aria-modal")).toBe("true");
      expect(inner.getAttribute("aria-hidden")).toBeNull();
      expect(inner.hasAttribute("inert")).toBe(false);

      fireEvent.keyDown(document, { key: "Escape" });
      expect(outer.getAttribute("aria-hidden")).toBe("true");
      expect(outer.hasAttribute("inert")).toBe(true);
      expect(inner.getAttribute("aria-modal")).toBe("true");
      runExit();

      expect(screen.getByRole("dialog")).toBe(outer);
      expect(outer.getAttribute("aria-modal")).toBe("true");
      expect(outer.getAttribute("aria-hidden")).toBeNull();
      expect(outer.hasAttribute("inert")).toBe(false);
      expect(document.activeElement).toBe(opener);
    });

    it("restores the page opener when a whole nested stack unmounts together", () => {
      const pageOpener = document.createElement("button");
      document.body.append(pageOpener);
      pageOpener.focus();
      try {
        const { view } = renderNested();

        view.unmount();

        expect(document.activeElement).toBe(pageOpener);
      } finally {
        pageOpener.remove();
      }
    });

    it("falls back to the outer dialog when the nested opener disappears", () => {
      const { outer, hideOpener } = renderNested();
      act(() => hideOpener());
      expect(screen.queryByRole("button", { name: "Delete", hidden: true })).toBeNull();

      fireEvent.keyDown(document, { key: "Escape" });
      runExit();

      expect(document.activeElement).toBe(outer);
      expect(outer.getAttribute("aria-modal")).toBe("true");
      expect(outer.hasAttribute("inert")).toBe(false);
    });

    it("gives Escape to the inner dialog alone", () => {
      const { onOuterClose, onInnerClose } = renderNested();

      fireEvent.keyDown(document, { key: "Escape" });
      runExit();

      expect(onInnerClose).toHaveBeenCalledTimes(1);
      expect(onOuterClose).not.toHaveBeenCalled();
      // The outer dialog is still there, and now answers Escape itself.
      expect(screen.getAllByRole("dialog")).toHaveLength(1);

      fireEvent.keyDown(document, { key: "Escape" });
      runExit();
      expect(onOuterClose).toHaveBeenCalledTimes(1);
    });

    it("leaves the outer veil inert while the inner dialog is up", () => {
      const { onOuterClose, onInnerClose, inner } = renderNested();

      // The inner veil sits over the outer one, so this is the click a member
      // lands when aiming past the confirm.
      clickVeil(inner);
      runExit();

      expect(onInnerClose).toHaveBeenCalledTimes(1);
      expect(onOuterClose).not.toHaveBeenCalled();
    });

    it("stops the outer focus trap from cycling past the dialog on top of it", () => {
      // jsdom lays nothing out, so every element's offsetParent is null and the
      // trap's visibility filter would drop all of its items. Stand one in.
      const offsetParent = Object.getOwnPropertyDescriptor(HTMLElement.prototype, "offsetParent");
      Object.defineProperty(HTMLElement.prototype, "offsetParent", {
        configurable: true,
        get: () => document.body,
      });
      try {
        renderNested();
        const last = screen.getByRole("button", { name: "Close outer", hidden: true });
        last.focus();

        fireEvent.keyDown(document, { key: "Tab" });

        // Tabbing off the outer surface's last item used to wrap back to its
        // first; with a dialog on top, the outer surface owns no Tab at all.
        expect(document.activeElement).toBe(last);
        expect(document.activeElement).not.toBe(
          screen.getByRole("button", { name: "Delete", hidden: true }),
        );
      } finally {
        if (offsetParent) Object.defineProperty(HTMLElement.prototype, "offsetParent", offsetParent);
        else delete (HTMLElement.prototype as unknown as Record<string, unknown>).offsetParent;
      }
    });

    /* The rule is the shared machine's, not the Modal's: a Menu opened from
       inside a dialog is just another surface on the stack, and the one on top
       is the one Escape reaches. */
    it("gives Escape to a menu opened inside the dialog, not the dialog", () => {
      const onClose = vi.fn();
      render(
        <Modal label="Test modal" onClose={onClose}>
          {() => <Menu label="More actions" actions={[{ label: "Edit", onSelect: () => {} }]} />}
        </Modal>,
      );
      const dialog = screen.getByRole("dialog");
      act(() => screen.getByRole("button", { name: "More actions" }).click());
      expect(screen.getByRole("menu")).not.toBeNull();
      expect(dialog.getAttribute("aria-modal")).toBe("true");
      expect(dialog.getAttribute("aria-hidden")).toBeNull();
      expect(dialog.hasAttribute("inert")).toBe(false);

      fireEvent.keyDown(document, { key: "Escape" });
      runExit();

      expect(screen.queryByRole("menu")).toBeNull();
      expect(onClose).not.toHaveBeenCalled();

      // With the menu gone the dialog is back on top and takes the next press.
      fireEvent.keyDown(document, { key: "Escape" });
      runExit();
      expect(onClose).toHaveBeenCalledTimes(1);
    });

    it("holds the page-owner scroll lock until the last dialog is gone", () => {
      document.body.style.overflow = "visible";
      const { onInnerClose } = renderNested();

      expect(document.body.style.overflow).toBe("hidden");

      fireEvent.keyDown(document, { key: "Escape" });
      runExit();
      expect(onInnerClose).toHaveBeenCalledTimes(1);
      // The outer dialog is still open: the page must not scroll behind it.
      expect(document.body.style.overflow).toBe("hidden");

      fireEvent.keyDown(document, { key: "Escape" });
      runExit();
      expect(document.body.style.overflow).toBe("visible");
    });
  });
});
