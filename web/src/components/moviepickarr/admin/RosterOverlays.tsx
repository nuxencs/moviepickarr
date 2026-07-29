import {
  CheckCircle2Icon,
  CopyIcon,
  KeyRoundIcon,
  LinkIcon,
  ShieldXIcon,
  TriangleAlertIcon,
} from "lucide-react";
import { useState } from "react";

import { loginChips, removeOutcome } from "@/components/moviepickarr/admin/roster";
import { Avatar } from "@/components/moviepickarr/Bits";
import { Modal } from "@/components/moviepickarr/Modal";
import { possessive } from "@/components/moviepickarr/possessive";
import { toast } from "@/components/ui/toast-api";


import type { RosterMember } from "@/types/Response";

// The avatar + name/handle cluster shown in the first column. Tags the current
// admin ("You") and any admin, so the acting admin can spot themselves for the
// self-guarded actions.
export function MemberIdentity({
  member,
  isSelf,
  size = 34,
  extras = [],
}: {
  member: RosterMember;
  isSelf: boolean;
  size?: number;
  /** The shed columns' values, shown on the sub-line below 640 only (roster.css).
   *  Rendered on every screen and hidden by CSS above the breakpoint, so nothing
   *  here depends on a measured width. */
  extras?: string[];
}) {
  return (
    <div className="adm-id">
      <Avatar name={member.name} size={size} />
      <div className="adm-id__text">
        <div className="adm-id__name">
          {member.name}
          {isSelf && <span className="adm-tag adm-tag--you">You</span>}
          {member.role === "admin" && <span className="adm-tag adm-tag--admin">Admin</span>}
        </div>
        <div className="adm-id__sub">
          {member.username ? `@${member.username}` : "no username"}
          {extras.map((extra) => (
            <span key={extra} className="adm-id__extra">
              {" · "}
              {extra}
            </span>
          ))}
        </div>
      </div>
    </div>
  );
}

// The presence-derived login-state chips. Icons only on the credential chips;
// the placeholder/archived states are label-only.
export function CredChips({ member }: { member: RosterMember }) {
  const chips = loginChips(member);
  return (
    <div className="adm-chips">
      {chips.map((chip) => (
        <span key={chip.kind} className={`adm-chip adm-chip--${chip.kind}`}>
          {chip.kind === "password" && <KeyRoundIcon />}
          {chip.kind === "sso" && <LinkIcon />}
          {chip.label}
        </span>
      ))}
    </div>
  );
}

// The one-time claim URL. The ceremony IS the design: shown once, no resend, so
// the copy affordance and the "won't be shown again" warning carry the weight.
// claimUrl is a relative /claim/<token> path; the copied value is absolute.
export function InviteReveal({
  name,
  claimUrl,
  onClose,
}: {
  name: string;
  claimUrl: string;
  onClose: () => void;
}) {
  const [copied, setCopied] = useState(false);
  const absolute = `${window.location.origin}${claimUrl}`;

  const copy = async () => {
    try {
      await navigator.clipboard.writeText(absolute);
      setCopied(true);
    } catch {
      toast.error("Couldn't copy. Select the link and copy it manually.");
    }
  };

  return (
    <Modal onClose={onClose} className="modal--form">
      {(close) => (
        <div className="adm-sheet adm-invite">
          <div className="adm-invite__head">
            <span className="adm-invite__icon">
              <CheckCircle2Icon />
            </span>
            <div>
              <h3 className="adm-modal__title">Invite ready for {name}</h3>
              <p className="adm-modal__sub">
                Send this link to {name} so they can set a password or link SSO.
              </p>
            </div>
          </div>

          <div className="adm-invite__urlrow">
            <code className="adm-invite__url">{absolute}</code>
            <button
              type="button"
              className="btn btn--accent adm-invite__copy"
              data-copied={copied}
              onClick={copy}
            >
              <CopyIcon />
              {copied ? "Copied" : "Copy"}
            </button>
          </div>

          <div className="adm-note" data-tone="warn">
            <TriangleAlertIcon />
            <span>
              Copy it now. This is the only time it's shown, and there's no resend. If
              it's lost, regenerate a fresh link (which invalidates this one).
            </span>
          </div>

          <div className="adm-modal__actions">
            <button type="button" className="btn btn--ghost" onClick={close}>
              Done
            </button>
          </div>
        </div>
      )}
    </Modal>
  );
}

// Remove is one action with two outcomes decided by attribution. The confirm
// names which will happen before the admin commits: a clean delete that frees
// the name, or an archive that keeps the row for its movie attribution.
export function RemoveConfirm({
  member,
  pending,
  onConfirm,
  onClose,
}: {
  member: RosterMember;
  pending: boolean;
  onConfirm: () => void;
  onClose: () => void;
}) {
  const isDelete = removeOutcome(member) === "delete";
  return (
    <Modal onClose={onClose} className="modal--form" dismissible={!pending}>
      {(close) => (
        <div className="adm-sheet adm-confirm">
          <span className="adm-confirm__icon" data-tone={isDelete ? "error" : "warn"}>
            <TriangleAlertIcon />
          </span>
          <h3 className="adm-modal__title">Remove {member.name}?</h3>

          {isDelete ? (
            <p className="adm-modal__sub">
              {member.name} hasn't added any movies, so this is a clean delete. The row is
              gone and the name <strong>{member.name}</strong> frees up for reuse.
            </p>
          ) : (
            <p className="adm-modal__sub">
              {member.name} added <strong>{member.moviesAuthored} movies</strong>. Removing
              keeps the row so those stay credited to them, but strips their login and hides
              them from the active roster. This is an <strong>archive</strong>, not a delete.
            </p>
          )}

          <div className="adm-modal__actions">
            <button type="button" className="btn btn--ghost" onClick={close} disabled={pending}>
              Cancel
            </button>
            <button
              type="button"
              className={isDelete ? "btn btn--danger" : "btn btn--accent"}
              onClick={onConfirm}
              disabled={pending}
            >
              {pending ? "Removing…" : isDelete ? "Delete member" : "Archive member"}
            </button>
          </div>
        </div>
      )}
    </Modal>
  );
}

// The self-last-credential guard: unlinking your only way in would lock you out.
// Refused client-side before the round trip; the server 409 is the backstop.
export function UnlinkGuard({ onClose }: { onClose: () => void }) {
  return (
    <Modal onClose={onClose} className="modal--form">
      {(close) => (
        <div className="adm-sheet adm-confirm">
          <span className="adm-confirm__icon" data-tone="error">
            <KeyRoundIcon />
          </span>
          <h3 className="adm-modal__title">Can't unlink SSO</h3>
          <p className="adm-modal__sub">
            SSO is your only way in, so unlinking it would lock you out of your own admin
            account. Set a password first, then you can unlink.
          </p>
          <div className="adm-modal__actions">
            <button type="button" className="btn btn--accent" onClick={close}>
              Got it
            </button>
          </div>
        </div>
      )}
    </Modal>
  );
}

// Set (create) or reset a member's local login. A placeholder that has SSO but
// no password gets one here; an existing local login is reset (which revokes the
// member's other sessions server-side).
export function SetLoginDialog({
  member,
  pending,
  onSubmit,
  onClose,
}: {
  member: RosterMember;
  pending: boolean;
  onSubmit: (username: string, password: string) => void;
  onClose: () => void;
}) {
  const isReset = member.hasLocalLogin;
  const [username, setUsername] = useState(member.username ?? "");
  const [password, setPassword] = useState("");
  const canSubmit = username.trim().length > 0 && password.length > 0 && !pending;

  return (
    <Modal onClose={onClose} className="modal--form" dismissible={!pending}>
      {(close) => (
        <form
          className="adm-sheet"
          onSubmit={(e) => {
            e.preventDefault();
            if (canSubmit) onSubmit(username.trim(), password);
          }}
        >
          <h3 className="adm-modal__title">
            {isReset ? `Reset ${possessive(member.name)} password` : `Set a password for ${member.name}`}
          </h3>
          <p className="adm-modal__sub">
            {isReset
              ? "This replaces their current password and signs them out of every other device."
              : "Gives them a username and password to log in with, alongside any SSO."}
          </p>

          <div className="adm-form">
            <label className="field">
              <input
                value={username}
                onChange={(e) => setUsername(e.target.value)}
                placeholder="Username"
                aria-label="Username"
                autoComplete="off"
                autoFocus={!isReset}
              />
            </label>
            <label className="field">
              <input
                type="password"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                placeholder="New password"
                aria-label="New password"
                autoComplete="new-password"
                autoFocus={isReset}
              />
            </label>
          </div>

          <div className="adm-modal__actions">
            <button type="button" className="btn btn--ghost" onClick={close} disabled={pending}>
              Cancel
            </button>
            <button type="submit" className="btn btn--accent" disabled={!canSubmit}>
              {pending ? "Saving…" : isReset ? "Reset password" : "Set password"}
            </button>
          </div>
        </form>
      )}
    </Modal>
  );
}

// A plain member who reached the admin URL. A first-class forbidden state, not a
// 404 mask: the page exists, the role doesn't.
export function ForbiddenState({ onLeave }: { onLeave: () => void }) {
  return (
    <div className="adm-forbidden">
      <span className="adm-forbidden__icon">
        <ShieldXIcon />
      </span>
      <h1 className="adm-forbidden__title">Admins only</h1>
      <p className="adm-forbidden__lead">
        Managing the roster is an admin job. Your account doesn't have that role, so this
        page is off-limits. If you think that's wrong, ask an admin.
      </p>
      <button type="button" className="btn btn--accent adm-forbidden__btn" onClick={onLeave}>
        Back to movie night
      </button>
    </div>
  );
}
