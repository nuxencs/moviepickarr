import { KeyRoundIcon, LogOutIcon, MonitorSmartphoneIcon, ShieldAlertIcon, TriangleAlertIcon } from "lucide-react";
import { FormEvent, useState } from "react";

import {
  otherDevicesLabel,
  PROVIDER,
  validateChangePassword,
  validateSetPassword,
} from "@/components/moviepickarr/account/account";
import { Modal } from "@/components/moviepickarr/Modal";

function DialogError({ message }: { message: string | null }) {
  if (!message) return null;
  return (
    <div className="acc-dialogerr" role="alert">
      <TriangleAlertIcon />
      <span>{message}</span>
    </div>
  );
}

// The warn note shows up front that other devices get signed out.
export function ChangePasswordDialog({
  pending,
  serverError,
  onSubmit,
  onClose,
}: {
  pending: boolean;
  serverError: string | null;
  onSubmit: (current: string, next: string) => void;
  onClose: () => void;
}) {
  const [current, setCurrent] = useState("");
  const [next, setNext] = useState("");
  const [confirm, setConfirm] = useState("");
  const [error, setError] = useState<string | null>(null);

  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (pending) return;
    const problem = validateChangePassword(current, next, confirm);
    if (problem) {
      setError(problem);
      return;
    }
    setError(null);
    onSubmit(current, next);
  };

  return (
    <Modal label="Change password" onClose={onClose} className="modal--form" dismissible={!pending}>
      {(close) => (
        <form className="acc-sheet" onSubmit={submit}>
          <div className="acc-dialoghead">
            <span className="acc-dialogicon">
              <KeyRoundIcon />
            </span>
            <div>
              <h3 className="acc-modal__title">Change password</h3>
              <p className="acc-modal__sub">Enter your current password, then choose a new one.</p>
            </div>
          </div>

          <div className="acc-form">
            <label className="field">
              <input
                type="password"
                value={current}
                onChange={(e) => {
                  setCurrent(e.target.value);
                  setError(null);
                }}
                placeholder="Current password"
                aria-label="Current password"
                autoComplete="current-password"
                autoFocus
              />
            </label>
            <label className="field">
              <input
                type="password"
                value={next}
                onChange={(e) => {
                  setNext(e.target.value);
                  setError(null);
                }}
                placeholder="New password (at least 8 characters)"
                aria-label="New password"
                autoComplete="new-password"
              />
            </label>
            <label className="field">
              <input
                type="password"
                value={confirm}
                onChange={(e) => {
                  setConfirm(e.target.value);
                  setError(null);
                }}
                placeholder="Confirm new password"
                aria-label="Confirm new password"
                autoComplete="new-password"
              />
            </label>
          </div>

          <DialogError message={error ?? serverError} />

          <div className="acc-note" data-tone="warn">
            <MonitorSmartphoneIcon />
            <span>This signs you out on every other device. You&apos;ll stay signed in here.</span>
          </div>

          <div className="acc-modal__actions">
            <button type="button" className="btn btn--ghost" onClick={close} disabled={pending}>
              Cancel
            </button>
            <button type="submit" className="btn btn--accent" disabled={pending}>
              {pending ? "Updating…" : "Update password"}
            </button>
          </div>
        </form>
      )}
    </Modal>
  );
}

// SSO-first members also pick a username, immutable once set (as in the claim flow).
export function SetPasswordDialog({
  pending,
  serverError,
  onSubmit,
  onClose,
}: {
  pending: boolean;
  serverError: string | null;
  onSubmit: (username: string, password: string) => void;
  onClose: () => void;
}) {
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [confirm, setConfirm] = useState("");
  const [error, setError] = useState<string | null>(null);

  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (pending) return;
    const problem = validateSetPassword(username.trim(), password, confirm);
    if (problem) {
      setError(problem);
      return;
    }
    setError(null);
    onSubmit(username.trim(), password);
  };

  return (
    <Modal label="Set a password" onClose={onClose} className="modal--form" dismissible={!pending}>
      {(close) => (
        <form className="acc-sheet" onSubmit={submit}>
          <div className="acc-dialoghead">
            <span className="acc-dialogicon">
              <KeyRoundIcon />
            </span>
            <div>
              <h3 className="acc-modal__title">Set a password</h3>
              <p className="acc-modal__sub">
                You sign in with {PROVIDER}. Add a password as a second way in, handy if {PROVIDER} is ever
                unavailable.
              </p>
            </div>
          </div>

          <div className="acc-form">
            <label className="field">
              <input
                value={username}
                onChange={(e) => {
                  setUsername(e.target.value);
                  setError(null);
                }}
                placeholder="Pick a username"
                aria-label="Username"
                autoComplete="username"
                autoFocus
              />
            </label>
            <label className="field">
              <input
                type="password"
                value={password}
                onChange={(e) => {
                  setPassword(e.target.value);
                  setError(null);
                }}
                placeholder="Password (at least 8 characters)"
                aria-label="Password"
                autoComplete="new-password"
              />
            </label>
            <label className="field">
              <input
                type="password"
                value={confirm}
                onChange={(e) => {
                  setConfirm(e.target.value);
                  setError(null);
                }}
                placeholder="Confirm password"
                aria-label="Confirm password"
                autoComplete="new-password"
              />
            </label>
          </div>

          <p className="acc-form__hint">You&apos;ll type this username to log in. It can&apos;t be changed later.</p>

          <DialogError message={error ?? serverError} />

          <div className="acc-modal__actions">
            <button type="button" className="btn btn--ghost" onClick={close} disabled={pending}>
              Cancel
            </button>
            <button type="submit" className="btn btn--accent" disabled={pending}>
              {pending ? "Saving…" : "Set password"}
            </button>
          </div>
        </form>
      )}
    </Modal>
  );
}

// Revokes every session, this device included.
export function LogoutEverywhereDialog({
  otherSessions,
  pending,
  onConfirm,
  onClose,
}: {
  otherSessions: number;
  pending: boolean;
  onConfirm: () => void;
  onClose: () => void;
}) {
  return (
    <Modal label="Log out everywhere?" onClose={onClose} className="modal--form" dismissible={!pending}>
      {(close) => (
        <div className="acc-sheet acc-confirm">
          <span className="acc-confirm__icon" data-tone="warn">
            <LogOutIcon />
          </span>
          <h3 className="acc-modal__title">Log out everywhere?</h3>
          <p className="acc-modal__sub">
            {otherSessions > 0 ? (
              <>
                You&apos;re signed in on <strong>{otherDevicesLabel(otherSessions)}</strong> besides this one.
                This ends every session, including this one, so you&apos;ll need to sign in again.
              </>
            ) : (
              <>This ends every session for your account, including this one. You&apos;ll need to sign in again.</>
            )}
          </p>
          <div className="acc-modal__actions">
            <button type="button" className="btn btn--ghost" onClick={close} disabled={pending}>
              Cancel
            </button>
            <button type="button" className="btn btn--danger" onClick={onConfirm} disabled={pending}>
              {pending ? "Logging out…" : "Log out everywhere"}
            </button>
          </div>
        </div>
      )}
    </Modal>
  );
}

// Points at the fix (set a password) instead of dead-ending; the server 409s too.
export function UnlinkGuardDialog({
  onSetPassword,
  onClose,
}: {
  onSetPassword: () => void;
  onClose: () => void;
}) {
  return (
    <Modal label={`Can't unlink ${PROVIDER}`} onClose={onClose} className="modal--form">
      {(close) => (
        <div className="acc-sheet acc-confirm">
          <span className="acc-confirm__icon" data-tone="error">
            <ShieldAlertIcon />
          </span>
          <h3 className="acc-modal__title">Can&apos;t unlink {PROVIDER}</h3>
          <p className="acc-modal__sub">
            {PROVIDER} is your only way to sign in, so unlinking it would lock you out of your own account. Set
            a password first, then you can unlink.
          </p>
          <div className="acc-modal__actions">
            <button type="button" className="btn btn--ghost" onClick={close}>
              Cancel
            </button>
            <button type="button" className="btn btn--accent" onClick={onSetPassword}>
              Set a password
            </button>
          </div>
        </div>
      )}
    </Modal>
  );
}
