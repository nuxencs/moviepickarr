/* ============================================================
   Render tests for the roster ceremonies (#140).

   Same shape as the account dialogs: controlled, provider-free, and the thing
   with no seam below the render is the wiring between props and the shared
   Modal. Two behaviours here carry real consequence and only exist once the
   component renders.

   The remove confirm decides between a clean delete and an archive. Which one
   applies is `removeOutcome`, and roster.test.ts owns that matrix. But an admin
   acts on the copy and the button label, not on the function, so what's
   asserted here is that the answer reaches both and they can't disagree.

   The invite reveal is a copy-or-lose ceremony: the link is shown once, with
   no resend. If the copy affordance silently fails the invite is gone, so the
   clipboard write and the acknowledgement it flips to are worth pinning.

   Modal.render.test.tsx covers the shell's own Escape / veil / pin behaviour;
   what's checked here is only whether these dialogs pass `pending` down to it.

   Unlike the account dialogs there's no inline error row to assert on: the
   roster surface reports failures through a toast, so a refused save leaves
   these components unchanged and there's nothing in the DOM to catch. The
   submit gate (`canSubmit`) is the equivalent guard, and that is covered.
   ============================================================ */

import { act, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import {
  InviteReveal,
  RemoveConfirm,
  SetLoginDialog,
} from "@/components/moviepickarr/admin/RosterOverlays";

import type { RosterMember } from "@/types/Response";

/** Long enough to outrun exitDelayMs(), whatever the motion tokens say. */
const AFTER_EXIT = 1000;

function runExit() {
  act(() => void vi.advanceTimersByTime(AFTER_EXIT));
}

function member(overrides: Partial<RosterMember> = {}): RosterMember {
  return {
    id: 7,
    name: "Cleo",
    username: "cleo",
    role: "member",
    archived: false,
    hasLocalLogin: false,
    hasLinkedIdentity: false,
    invitePending: false,
    moviesAuthored: 0,
    ...overrides,
  };
}

function button(name: string) {
  return screen.getByRole("button", { name });
}

beforeEach(() => vi.useFakeTimers());
afterEach(() => {
  vi.useRealTimers();
  vi.restoreAllMocks();
});

describe("RemoveConfirm", () => {
  function renderConfirm(m: RosterMember, pending = false) {
    const onConfirm = vi.fn();
    const onClose = vi.fn();
    render(<RemoveConfirm member={m} pending={pending} onConfirm={onConfirm} onClose={onClose} />);
    return { onConfirm, onClose, dialog: screen.getByRole("dialog") };
  }

  it("offers a delete, and says the name frees up, for a member with no movies", () => {
    const { dialog } = renderConfirm(member({ moviesAuthored: 0 }));

    expect(button("Delete member")).not.toBeNull();
    expect(dialog.textContent).toContain("clean delete");
  });

  it("offers an archive, and says the credits survive, once they've added movies", () => {
    const { dialog } = renderConfirm(member({ moviesAuthored: 3 }));

    expect(button("Archive member")).not.toBeNull();
    expect(dialog.textContent).toContain("3 movies");
    // The distinction the copy exists to make: this is not a delete.
    expect(dialog.textContent).toContain("archive");
    expect(screen.queryByRole("button", { name: "Delete member" })).toBeNull();
  });

  it("confirms through to the page", () => {
    const { onConfirm } = renderConfirm(member());

    fireEvent.click(button("Delete member"));

    expect(onConfirm).toHaveBeenCalledTimes(1);
  });

  it("disables both ways out while the removal is in flight", () => {
    renderConfirm(member(), true);

    expect(button("Removing…").hasAttribute("disabled")).toBe(true);
    expect(button("Cancel").hasAttribute("disabled")).toBe(true);
  });

  it("pins itself open mid-removal, so Escape can't dismiss a running request", () => {
    const { onClose } = renderConfirm(member(), true);

    fireEvent.keyDown(document, { key: "Escape" });
    runExit();

    expect(onClose).not.toHaveBeenCalled();
    expect(screen.queryByRole("dialog")).not.toBeNull();
  });
});

describe("SetLoginDialog", () => {
  function renderDialog(m: RosterMember, pending = false) {
    const onSubmit = vi.fn();
    const onClose = vi.fn();
    render(<SetLoginDialog member={m} pending={pending} onSubmit={onSubmit} onClose={onClose} />);
    return { onSubmit, onClose };
  }

  it("opens as a reset, prefilled, for a member who already has a login", () => {
    renderDialog(member({ hasLocalLogin: true, username: "cleo" }));

    expect(screen.getByRole("heading").textContent).toContain("Reset");
    expect((screen.getByLabelText("Username") as HTMLInputElement).value).toBe("cleo");
  });

  it("opens as a first set for a placeholder", () => {
    renderDialog(member({ hasLocalLogin: false, username: undefined }));

    expect(screen.getByRole("heading").textContent).toContain("Set a password for Cleo");
    expect((screen.getByLabelText("Username") as HTMLInputElement).value).toBe("");
  });

  it("holds the submit shut until both fields are filled", () => {
    renderDialog(member({ username: undefined }));

    expect(button("Set password").hasAttribute("disabled")).toBe(true);

    fireEvent.change(screen.getByLabelText("Username"), { target: { value: "cleo" } });
    expect(button("Set password").hasAttribute("disabled")).toBe(true);

    fireEvent.change(screen.getByLabelText("New password"), { target: { value: "hunter2" } });
    expect(button("Set password").hasAttribute("disabled")).toBe(false);
  });

  it("treats a whitespace-only username as unfilled", () => {
    renderDialog(member({ username: undefined }));

    fireEvent.change(screen.getByLabelText("Username"), { target: { value: "   " } });
    fireEvent.change(screen.getByLabelText("New password"), { target: { value: "hunter2" } });

    expect(button("Set password").hasAttribute("disabled")).toBe(true);
  });

  it("trims the username before handing it over", () => {
    const { onSubmit } = renderDialog(member({ username: undefined }));

    fireEvent.change(screen.getByLabelText("Username"), { target: { value: "  cleo  " } });
    fireEvent.change(screen.getByLabelText("New password"), { target: { value: "hunter2" } });
    fireEvent.click(button("Set password"));

    expect(onSubmit).toHaveBeenCalledWith("cleo", "hunter2");
  });

  it("disables the submit and pins itself while the save is in flight", () => {
    const { onClose } = renderDialog(member({ hasLocalLogin: true }), true);

    expect(button("Saving…").hasAttribute("disabled")).toBe(true);
    expect(button("Cancel").hasAttribute("disabled")).toBe(true);

    fireEvent.keyDown(document, { key: "Escape" });
    runExit();
    expect(onClose).not.toHaveBeenCalled();
  });
});

describe("InviteReveal", () => {
  // jsdom ships no clipboard, so it's defined rather than spied on, and has to
  // be taken back off afterwards: restoreAllMocks doesn't undo defineProperty.
  afterEach(() => {
    Reflect.deleteProperty(navigator, "clipboard");
  });

  function renderReveal(writeText = vi.fn(() => Promise.resolve())) {
    Object.defineProperty(navigator, "clipboard", {
      value: { writeText },
      configurable: true,
    });
    const onClose = vi.fn();
    render(<InviteReveal name="Cleo" claimUrl="/claim/tok3n" onClose={onClose} />);
    return { writeText, onClose };
  }

  it("shows the link as an absolute URL, ready to send", () => {
    renderReveal();

    expect(screen.getByRole("dialog").textContent).toContain(
      `${window.location.origin}/claim/tok3n`,
    );
  });

  it("copies the absolute link, not the relative path", async () => {
    const { writeText } = renderReveal();

    await act(async () => {
      fireEvent.click(button("Copy"));
    });

    expect(writeText).toHaveBeenCalledWith(`${window.location.origin}/claim/tok3n`);
  });

  it("acknowledges the copy, so the admin knows the one-time link is safe", async () => {
    renderReveal();

    await act(async () => {
      fireEvent.click(button("Copy"));
    });

    expect(button("Copied")).not.toBeNull();
  });

  it("stays on Copy when the clipboard refuses, instead of claiming a copy that never happened", async () => {
    renderReveal(vi.fn(() => Promise.reject(new Error("denied"))));

    await act(async () => {
      fireEvent.click(button("Copy"));
    });

    expect(button("Copy")).not.toBeNull();
  });
});

// UnlinkGuard has no test of its own: it's static copy over a Modal whose
// close path Modal.render.test.tsx already covers, and it takes no state that
// could be wired wrong. RosterPage.render.test.tsx covers reaching it.
