/* Render tests for the account ceremonies (#140), rendered from props alone.
   They pin the prop wiring to Modal: inline server errors, and `pending`
   reaching `dismissible` so Escape cannot dismiss a save mid-flight (the shell's
   own test stays green if a dialog drops it). Validation is account.test.ts's. */

import { act, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import {
  ChangePasswordDialog,
  LogoutEverywhereDialog,
  SetPasswordDialog,
  UnlinkGuardDialog,
} from "@/components/moviepickarr/account/AccountOverlays";

/** Long enough to outrun exitDelayMs(), whatever the motion tokens say. */
const AFTER_EXIT = 1000;

function runExit() {
  act(() => void vi.advanceTimersByTime(AFTER_EXIT));
}

/** The veil is the dialog's parent; a member clicks it to dismiss. */
function veil() {
  return screen.getByRole("dialog").parentElement as HTMLElement;
}

function fill(label: string, value: string) {
  fireEvent.change(screen.getByLabelText(label), { target: { value } });
}

function button(name: string) {
  return screen.getByRole("button", { name });
}

/** The inline error row both credential forms share. */
function inlineError() {
  return screen.queryByRole("alert");
}

beforeEach(() => vi.useFakeTimers());
afterEach(() => vi.useRealTimers());

describe("ChangePasswordDialog", () => {
  function renderDialog({ pending = false, serverError = null as string | null } = {}) {
    const onSubmit = vi.fn();
    const onClose = vi.fn();
    render(
      <ChangePasswordDialog
        pending={pending}
        serverError={serverError}
        onSubmit={onSubmit}
        onClose={onClose}
      />,
    );
    return { onSubmit, onClose };
  }

  it("opens on its fields, with nothing to correct yet", () => {
    renderDialog();

    expect(screen.getByRole("dialog")).not.toBeNull();
    expect(screen.getByLabelText("Current password")).not.toBeNull();
    expect(inlineError()).toBeNull();
  });

  it("shows the refusal inline instead of submitting it", () => {
    const { onSubmit } = renderDialog();

    fill("Current password", "hunter2");
    fill("New password", "short");
    fill("Confirm new password", "short");
    fireEvent.click(button("Update password"));

    expect(inlineError()?.textContent).toContain("at least 8 characters");
    expect(onSubmit).not.toHaveBeenCalled();
  });

  it("clears the refusal as soon as the member starts fixing it", () => {
    renderDialog();

    fireEvent.click(button("Update password"));
    expect(inlineError()).not.toBeNull();

    fill("Current password", "h");
    expect(inlineError()).toBeNull();
  });

  it("hands a good password to the page", () => {
    const { onSubmit } = renderDialog();

    fill("Current password", "hunter2");
    fill("New password", "longenough");
    fill("Confirm new password", "longenough");
    fireEvent.click(button("Update password"));

    expect(onSubmit).toHaveBeenCalledWith("hunter2", "longenough");
  });

  it("surfaces what the server said when the page passes it down", () => {
    renderDialog({ serverError: "Current password is wrong." });

    expect(inlineError()?.textContent).toContain("Current password is wrong.");
  });

  it("disables both ways out while the save is in flight", () => {
    renderDialog({ pending: true });

    expect(button("Updating…").hasAttribute("disabled")).toBe(true);
    expect(button("Cancel").hasAttribute("disabled")).toBe(true);
  });

  it("refuses a resubmit while pending, so a save can't be double-fired", () => {
    const { onSubmit } = renderDialog({ pending: true });

    fill("Current password", "hunter2");
    fill("New password", "longenough");
    fill("Confirm new password", "longenough");
    // The button is disabled, so submit the form to reach the guard an Enter keypress hits.
    fireEvent.submit(screen.getByRole("dialog").querySelector("form") as HTMLFormElement);

    expect(onSubmit).not.toHaveBeenCalled();
  });

  it("pins itself open mid-save, so Escape can't dismiss a running request", () => {
    const { onClose } = renderDialog({ pending: true });

    fireEvent.keyDown(document, { key: "Escape" });
    fireEvent.mouseDown(veil());
    fireEvent.mouseUp(veil());
    runExit();

    expect(onClose).not.toHaveBeenCalled();
    expect(screen.queryByRole("dialog")).not.toBeNull();
  });

  it("is dismissible again once the save is done", () => {
    const { onClose } = renderDialog({ pending: false });

    fireEvent.keyDown(document, { key: "Escape" });
    runExit();

    expect(onClose).toHaveBeenCalledTimes(1);
  });
});

describe("SetPasswordDialog", () => {
  function renderDialog({ pending = false, serverError = null as string | null } = {}) {
    const onSubmit = vi.fn();
    const onClose = vi.fn();
    render(
      <SetPasswordDialog
        pending={pending}
        serverError={serverError}
        onSubmit={onSubmit}
        onClose={onClose}
      />,
    );
    return { onSubmit, onClose };
  }

  it("shows the refusal inline instead of submitting it", () => {
    const { onSubmit } = renderDialog();

    fill("Username", "cleo");
    fill("Password", "longenough");
    fill("Confirm password", "different");
    fireEvent.click(button("Set password"));

    expect(inlineError()).not.toBeNull();
    expect(onSubmit).not.toHaveBeenCalled();
  });

  it("trims the username before handing it over, so a stray space isn't part of it", () => {
    const { onSubmit } = renderDialog();

    fill("Username", "  cleo  ");
    fill("Password", "longenough");
    fill("Confirm password", "longenough");
    fireEvent.click(button("Set password"));

    expect(onSubmit).toHaveBeenCalledWith("cleo", "longenough");
  });

  it("disables both ways out while the save is in flight", () => {
    renderDialog({ pending: true });

    expect(button("Saving…").hasAttribute("disabled")).toBe(true);
    expect(button("Cancel").hasAttribute("disabled")).toBe(true);
  });

  it("pins itself open mid-save", () => {
    const { onClose } = renderDialog({ pending: true });

    fireEvent.keyDown(document, { key: "Escape" });
    runExit();

    expect(onClose).not.toHaveBeenCalled();
  });
});

describe("LogoutEverywhereDialog", () => {
  function renderDialog({ otherSessions = 0, pending = false } = {}) {
    const onConfirm = vi.fn();
    const onClose = vi.fn();
    render(
      <LogoutEverywhereDialog
        otherSessions={otherSessions}
        pending={pending}
        onConfirm={onConfirm}
        onClose={onClose}
      />,
    );
    return { onConfirm, onClose };
  }

  it("makes the count concrete when other devices are signed in", () => {
    renderDialog({ otherSessions: 1 });

    expect(screen.getByRole("dialog").textContent).toContain("1 other device");
  });

  it("says nothing about a count when this is the only device", () => {
    renderDialog({ otherSessions: 0 });

    expect(screen.getByRole("dialog").textContent).not.toContain("other device");
  });

  it("confirms through to the page", () => {
    const { onConfirm } = renderDialog({ otherSessions: 2 });

    fireEvent.click(button("Log out everywhere"));

    expect(onConfirm).toHaveBeenCalledTimes(1);
  });

  it("pins itself open while the revoke is in flight", () => {
    const { onClose } = renderDialog({ pending: true });

    expect(button("Logging out…").hasAttribute("disabled")).toBe(true);
    fireEvent.keyDown(document, { key: "Escape" });
    runExit();

    expect(onClose).not.toHaveBeenCalled();
  });
});

describe("UnlinkGuardDialog", () => {
  it("points at the fix instead of dead-ending", () => {
    const onSetPassword = vi.fn();
    render(<UnlinkGuardDialog onSetPassword={onSetPassword} onClose={vi.fn()} />);

    fireEvent.click(button("Set a password"));

    expect(onSetPassword).toHaveBeenCalledTimes(1);
  });
});
