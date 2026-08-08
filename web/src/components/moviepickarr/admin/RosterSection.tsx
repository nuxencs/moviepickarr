import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import {
  EyeOffIcon,
  KeyRoundIcon,
  MailPlusIcon,
  PlusIcon,
  RotateCcwIcon,
  RotateCwIcon,
  ShieldIcon,
  ShieldOffIcon,
  Trash2Icon,
  UnlinkIcon,
  UsersIcon,
} from "lucide-react";
import { FormEvent, useEffect, useMemo, useRef, useState, type CSSProperties, type ReactNode } from "react";

import { APIClient, ApiError } from "@/api/APIClient";
import { reconcileInviteSurfaces } from "@/api/inviteCache";
import { InvitesQueryOptions, MeQueryOptions, RosterQueryOptions } from "@/api/queries";
import { MoviesKeys, UsersKeys } from "@/api/query_keys";

import { expiryLabel, inviteStatusAt, issuedLabel, nextInviteExpiryDelay, serverAlignedNow } from "@/components/moviepickarr/admin/invites";
import { credLabel, isPlaceholder, unlinkWouldStrand } from "@/components/moviepickarr/admin/roster";
import {
  CredChips,
  ForbiddenState,
  InviteReveal,
  MemberIdentity,
  RemoveConfirm,
  SetLoginDialog,
  UnlinkGuard,
} from "@/components/moviepickarr/admin/RosterOverlays";
import { plural } from "@/components/moviepickarr/lib";
import { Menu, type MenuAction } from "@/components/moviepickarr/Menu";
import { toast } from "@/components/ui/toast-api";

import type { InviteStatus, InviteSummary, RosterMember } from "@/types/Response";

import { timeAgo } from "@/lib/time";

import "@/components/moviepickarr/admin/roster.css";

/**
 * The data columns between the identity and the row kebab, in display order.
 *
 * Below 640 the table can't hold six columns on a phone, so the `shed` ones
 * hide and fold into the identity sub-line instead (roster.css). Both
 * renderings come from this one array: a column added here reaches the desktop
 * cell and the phone summary together, or neither. Which screen you are on is
 * CSS, the way it is everywhere else (cf. UsersTab's PUSH_WIDTH note).
 *
 * `summary` returns null where the phone line would only repeat the row: the
 * admin role is already a tag beside the name, "Member" is the default and goes
 * unsaid, and a member who has added nothing needs no zero.
 */
interface RosterColumn {
  key: string;
  header: string;
  className?: string;
  shed: boolean;
  cell: (m: RosterMember, inviteState: InviteCellState) => ReactNode;
  summary?: (m: RosterMember) => string | null;
}

const COLUMNS: RosterColumn[] = [
  {
    key: "role",
    header: "Role",
    shed: true,
    cell: (m) => <span className="adm-role">{m.role === "admin" ? "Admin" : "Member"}</span>,
    summary: () => null,
  },
  {
    key: "login",
    header: "Login",
    shed: false,
    cell: (m, inviteState) => <LoginCell member={m} state={inviteState} />,
  },
  {
    key: "movies",
    header: "Movies",
    className: "adm-num",
    shed: true,
    cell: (m) => m.moviesAuthored,
    summary: (m) => (m.moviesAuthored > 0 ? plural(m.moviesAuthored, "movie") : null),
  },
  {
    key: "seen",
    header: "Last active",
    className: "adm-muted",
    shed: true,
    cell: (m) => timeAgo(m.lastSeenAt) || "Never",
    summary: (m) => timeAgo(m.lastSeenAt) || null,
  },
];

/** The shed columns' phone line, e.g. "38 movies · now". */
const summaryOf = (m: RosterMember): string[] =>
  COLUMNS.filter((c) => c.shed)
    .map((c) => c.summary?.(m) ?? null)
    .filter((s): s is string => s !== null);

// Identity + the data columns + the kebab, for the states that span the row.
const COLUMN_COUNT = COLUMNS.length + 2;

// The one active ceremony. Only one is ever open, so a single tagged union keeps
// the modal orchestration a plain switch rather than a pile of booleans.
type Dialog =
  | {
      kind: "invite";
      name: string;
      claimUrl: string;
      purpose: "invite" | "password-reset";
    }
  | { kind: "remove"; member: RosterMember }
  | { kind: "unlink-guard" }
  | { kind: "set-login"; member: RosterMember }
  | null;

interface InviteCellState {
  now: number;
  invite?: InviteSummary;
  status?: InviteStatus;
  pendingLabel?: string;
  syncing?: boolean;
}

function LoginCell({ member, state }: { member: RosterMember; state: InviteCellState }) {
  if (member.archived) return <CredChips member={member} />;

  const placeholder = isPlaceholder(member);

  if (state.pendingLabel) {
    return (
      <div className="adm-login" aria-busy="true">
        {!placeholder && <CredChips member={member} />}
        <span
          className={placeholder ? "adm-chip adm-chip--pending" : "adm-login__invite"}
          role="status"
        >
          {state.pendingLabel}
        </span>
      </div>
    );
  }
  if (state.syncing) {
    return (
      <div className="adm-login" aria-busy="true">
        {!placeholder && <CredChips member={member} />}
        <span
          className={placeholder ? "adm-chip adm-chip--empty" : "adm-login__invite"}
          role="status"
        >
          {placeholder ? "Invite status updating…" : "Reset link status updating…"}
        </span>
      </div>
    );
  }
  if (!state.invite || !state.status) return <CredChips member={member} />;

  const issued = issuedLabel(state.invite, state.now);
  const timing = expiryLabel(state.invite, state.now, state.status);
  if (!placeholder) {
    return (
      <div className="adm-login">
        <CredChips member={member} />
        <span
          className="adm-login__invite"
          data-expired={state.status === "expired" || undefined}
        >
          Password reset link {state.status} · {timing}
          {issued ? ` · ${issued}` : ""}
        </span>
      </div>
    );
  }

  return (
    <div className="adm-login">
      <span
        className={`adm-chip adm-chip--${state.status === "open" ? "pending" : "expired"}`}
      >
        {state.status === "open" ? "Invite link open" : "Invite expired"}
      </span>
      <span className="adm-login__meta">
        {timing}
        {issued ? ` · ${issued}` : ""}
      </span>
    </div>
  );
}

// Every roster mutation toasts the server's message on failure (a 409 last-admin
// / self-lockout carries its reason in the ApiError message) and falls back to a
// generic line.
const fail = (fallback: string) => (err: unknown) =>
  toast.error(err instanceof ApiError && err.message ? err.message : fallback);

export function RosterSection() {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const { data: me } = useQuery(MeQueryOptions());
  const roster = useQuery(RosterQueryOptions());
  const invites = useQuery(InvitesQueryOptions());

  const [dialog, setDialog] = useState<Dialog>(null);
  const [clockTick, setClockTick] = useState(() => Date.now());

  const inviteByMember = useMemo(
    () => new Map((invites.data?.items ?? []).map((invite) => [invite.memberId, invite])),
    [invites.data?.items],
  );
  const inviteNow = invites.data
    ? serverAlignedNow(invites.data.serverNow, invites.dataUpdatedAt, clockTick)
    : clockTick;
  const projectionMismatches = useMemo(() => {
    if (!roster.data || !invites.data) return [];
    return roster.data.flatMap((member) => {
      if (member.archived) return [];
      const exact = inviteByMember.get(member.id);
      const exactStatus = exact ? inviteStatusAt(exact, inviteNow) : undefined;
      const exactOpen = exactStatus === "open";
      return member.invitePending !== exactOpen
        ? [{
            memberID: member.id,
            rosterOpen: member.invitePending,
            inviteID: exact?.id ?? null,
            exactStatus: exactStatus ?? null,
          }]
        : [];
    });
  }, [inviteByMember, inviteNow, invites.data, roster.data]);
  const projectionMismatchSet = useMemo(
    () => new Set(projectionMismatches.map((mismatch) => mismatch.memberID)),
    [projectionMismatches],
  );
  const projectionMismatchSignature = projectionMismatches.length > 0
    ? JSON.stringify(projectionMismatches)
    : "";
  const reconciledMismatch = useRef("");

  // Roster and invite overview are separate snapshots. If their shared "open"
  // projection disagrees, hide exact commands and refresh both once. Holding the
  // signature avoids a refetch loop when a backend or network fault persists.
  useEffect(() => {
    if (!projectionMismatchSignature) {
      reconciledMismatch.current = "";
      return;
    }
    if (invites.isError || reconciledMismatch.current === projectionMismatchSignature) return;
    reconciledMismatch.current = projectionMismatchSignature;
    void reconcileInviteSurfaces(queryClient);
  }, [invites.isError, projectionMismatchSignature, queryClient]);

  // Refresh relative wording once a minute, then hit the exact server-clock
  // expiry boundary when it comes first. Crossing expiry also reconciles the
  // roster and exact-handle overview in case another tab claimed the link.
  useEffect(() => {
    if (!invites.data) return;
    const minute = 60_000;
    const now = serverAlignedNow(invites.data.serverNow, invites.dataUpdatedAt, clockTick);
    const expiryDelay = nextInviteExpiryDelay(invites.data.items, now);
    const crossesExpiry = expiryDelay !== null && expiryDelay <= minute;
    const delay = crossesExpiry ? expiryDelay + 25 : minute;
    const timer = window.setTimeout(() => {
      setClockTick(Date.now());
      if (crossesExpiry) void reconcileInviteSurfaces(queryClient);
    }, delay);
    return () => window.clearTimeout(timer);
  }, [clockTick, invites.data, invites.dataUpdatedAt, queryClient]);

  const refresh = () => queryClient.invalidateQueries({ queryKey: UsersKeys.roster() });
  // Archive and restore change whether historical attribution is a live board
  // link. This is the mutation's fallback when its own SSE frame is lost.
  const refreshMovieAttribution = () =>
    queryClient.invalidateQueries({ queryKey: MoviesKeys.all });
  const closeDialog = () => setDialog(null);

  const createInvite = useMutation({
    mutationFn: ({ member, passwordReset }: { member: RosterMember; passwordReset: boolean }) =>
      passwordReset
        ? APIClient.members.createPasswordResetInvite(member.id)
        : APIClient.members.createInvite(member.id),
    onSuccess: (res, { member, passwordReset }) => {
      setDialog({
        kind: "invite",
        name: member.name,
        claimUrl: res.claimUrl,
        purpose: passwordReset ? "password-reset" : "invite",
      });
    },
    onError: fail("Couldn't create the invite link."),
    onSettled: () => reconcileInviteSurfaces(queryClient),
  });

  const replaceInvite = useMutation({
    mutationFn: ({ invite }: { member: RosterMember; invite: InviteSummary }) =>
      APIClient.invites.replace(invite.id),
    onSuccess: (res, { member }) => {
      setDialog({
        kind: "invite",
        name: member.name,
        claimUrl: res.claimUrl,
        purpose: isPlaceholder(member) ? "invite" : "password-reset",
      });
    },
    onError: fail("Couldn't create a replacement invite link."),
    onSettled: () => reconcileInviteSurfaces(queryClient),
  });

  const revokeInvite = useMutation({
    mutationFn: ({ invite }: { member: RosterMember; invite: InviteSummary }) =>
      APIClient.invites.revoke(invite.id),
    onSuccess: (_res, { member }) => {
      toast.success(`Invite for ${member.name} revoked`);
    },
    onError: fail("Couldn't revoke the invite link."),
    onSettled: () => reconcileInviteSurfaces(queryClient),
  });

  const dismissInvite = useMutation({
    mutationFn: ({ invite }: { member: RosterMember; invite: InviteSummary }) =>
      APIClient.invites.dismiss(invite.id),
    onSuccess: (_res, { member }) => {
      toast.success(`Dismissed ${member.name}'s expired invite`);
    },
    onError: fail("Couldn't dismiss the expired invite."),
    onSettled: () => reconcileInviteSurfaces(queryClient),
  });

  const setRole = useMutation({
    mutationFn: ({ member, role }: { member: RosterMember; role: "member" | "admin" }) =>
      APIClient.members.setRole(member.id, role),
    onSuccess: (_res, { member, role }) => {
      refresh();
      // Changing your own role changes your own nav (the admin Shield link, the
      // name tag), so refresh the session actor too, not just the roster row.
      if (me?.id === member.id) queryClient.invalidateQueries({ queryKey: ["auth", "me"] });
      toast.success(`${member.name} is now ${role === "admin" ? "an admin" : "a member"}`);
    },
    onError: fail("Couldn't change the role."),
  });

  const setLogin = useMutation({
    mutationFn: ({ member, username, password }: { member: RosterMember; username: string; password: string }) =>
      APIClient.members.setLocalLogin(member.id, username, password),
    onSuccess: (_res, { member }) => {
      closeDialog();
      toast.success(`Password ${member.hasLocalLogin ? "reset" : "set"} for ${member.name}`);
    },
    onError: fail("Couldn't save the password."),
    onSettled: () => reconcileInviteSurfaces(queryClient),
  });

  const removeLogin = useMutation({
    mutationFn: (member: RosterMember) => APIClient.members.removeLocalLogin(member.id),
    onSuccess: (_res, member) => {
      toast.success(`Password removed for ${member.name}`);
    },
    onError: fail("Couldn't remove the password."),
    onSettled: () => reconcileInviteSurfaces(queryClient),
  });

  const unlink = useMutation({
    mutationFn: ({ member, self }: { member: RosterMember; self: boolean }) =>
      self ? APIClient.members.unlinkSelf() : APIClient.members.unlink(member.id),
    onSuccess: (_res, { member, self }) => {
      if (self) queryClient.invalidateQueries({ queryKey: ["auth", "me"] });
      toast.success(`SSO unlinked for ${member.name}`);
    },
    onError: fail("Couldn't unlink SSO."),
    onSettled: () => reconcileInviteSurfaces(queryClient),
  });

  const removeMember = useMutation({
    mutationFn: (member: RosterMember) => APIClient.members.remove(member.id),
    onSuccess: (res, member) => {
      closeDialog();
      refreshMovieAttribution();
      toast.success(res.outcome === "deleted" ? `${member.name} deleted` : `${member.name} archived`);
    },
    onError: fail("Couldn't remove the member."),
    onSettled: () => reconcileInviteSurfaces(queryClient),
  });

  const restore = useMutation({
    mutationFn: (member: RosterMember) => APIClient.members.restore(member.id),
    onSuccess: (res, member) => {
      refreshMovieAttribution();
      setDialog({
        kind: "invite",
        name: member.name,
        claimUrl: res.claimUrl,
        purpose: "invite",
      });
    },
    onError: fail("Couldn't restore the member."),
    onSettled: () => reconcileInviteSurfaces(queryClient),
  });

  // A non-admin gets 403 from the roster read: render the first-class forbidden
  // state, never a 404 mask. Any other error is a genuine load failure.
  if (roster.isError) {
    if (roster.error instanceof ApiError && roster.error.status === 403) {
      return <ForbiddenState onLeave={() => navigate({ to: "/" })} />;
    }
    return <p className="adm-state">Couldn't load the roster. Try again in a moment.</p>;
  }
  if (invites.isError && !invites.data && invites.error instanceof ApiError && invites.error.status === 403) {
    return <ForbiddenState onLeave={() => navigate({ to: "/" })} />;
  }

  const members = roster.data ?? [];
  const active = members.filter((m) => !m.archived);
  const archived = members.filter((m) => m.archived);
  const activeAdmins = active.filter((m) => m.role === "admin").length;
  const isSelf = (m: RosterMember) => me?.id === m.id;
  const inviteActionBusy =
    createInvite.isPending ||
    replaceInvite.isPending ||
    revokeInvite.isPending ||
    dismissInvite.isPending;
  const inviteBusyMemberID = createInvite.isPending
    ? createInvite.variables?.member.id
    : replaceInvite.isPending
      ? replaceInvite.variables?.member.id
      : revokeInvite.isPending
        ? revokeInvite.variables?.member.id
        : dismissInvite.isPending
          ? dismissInvite.variables?.member.id
          : undefined;

  const invitePendingLabel = (memberID: number): string | undefined => {
    if (createInvite.isPending && createInvite.variables?.member.id === memberID) {
      return createInvite.variables.passwordReset ? "Creating reset link…" : "Creating link…";
    }
    if (replaceInvite.isPending && replaceInvite.variables?.member.id === memberID) return "Replacing link…";
    if (revokeInvite.isPending && revokeInvite.variables?.member.id === memberID) return "Revoking…";
    if (dismissInvite.isPending && dismissInvite.variables?.member.id === memberID) return "Dismissing…";
    return undefined;
  };

  const inviteStateFor = (member: RosterMember): InviteCellState => {
    const invite = inviteByMember.get(member.id);
    return {
      now: inviteNow,
      invite,
      status: invite ? inviteStatusAt(invite, inviteNow) : undefined,
      pendingLabel: invitePendingLabel(member.id),
      syncing: projectionMismatchSet.has(member.id),
    };
  };

  // The row kebab: invite commands stay beside the affected login state. A
  // password reset generation remains manageable for credentialed members.
  const rowActions = (m: RosterMember): MenuAction[] => {
    if (m.archived) {
      return [
        {
          icon: <RotateCcwIcon />,
          label: "Restore member",
          onSelect: () => restore.mutate(m),
        },
      ];
    }

    const actions: MenuAction[] = [];

    if (invites.data && !invites.isError && !projectionMismatchSet.has(m.id)) {
      const invite = inviteByMember.get(m.id);
      const status = invite ? inviteStatusAt(invite, inviteNow) : undefined;
      const reset = !isPlaceholder(m);
      if (!invite && !m.invitePending) {
        if (isPlaceholder(m)) {
          actions.push({
            icon: <MailPlusIcon />,
            label: "Create invite link",
            disabled: inviteActionBusy,
            onSelect: () => createInvite.mutate({ member: m, passwordReset: false }),
          });
        } else if (m.hasLocalLogin) {
          actions.push({
            icon: <MailPlusIcon />,
            label: "Create password reset link",
            disabled: inviteActionBusy,
            onSelect: () => createInvite.mutate({ member: m, passwordReset: true }),
          });
        }
      } else if (invite && status === "open") {
        actions.push(
          {
            icon: <RotateCwIcon />,
            label: reset ? "Create replacement reset link" : "Create replacement link",
            disabled: inviteActionBusy,
            onSelect: () => replaceInvite.mutate({ member: m, invite }),
          },
          {
            icon: <Trash2Icon />,
            label: reset ? "Revoke password reset link" : "Revoke invite link",
            disabled: inviteActionBusy,
            onSelect: () => revokeInvite.mutate({ member: m, invite }),
          },
        );
      } else if (invite) {
        actions.push(
          {
            icon: <MailPlusIcon />,
            label: reset ? "Create new password reset link" : "Create new invite link",
            disabled: inviteActionBusy,
            onSelect: () => replaceInvite.mutate({ member: m, invite }),
          },
          {
            icon: <EyeOffIcon />,
            label: reset ? "Dismiss expired reset link" : "Dismiss expired invite",
            disabled: inviteActionBusy,
            onSelect: () => dismissInvite.mutate({ member: m, invite }),
          },
        );
      }
    }

    if (!isPlaceholder(m)) {
      actions.push({
        icon: <KeyRoundIcon />,
        label: m.hasLocalLogin ? "Reset password" : "Set password",
        onSelect: () => setDialog({ kind: "set-login", member: m }),
      });
      if (m.hasLocalLogin) {
        actions.push({
          icon: <Trash2Icon />,
          label: "Remove password",
          onSelect: () => removeLogin.mutate(m),
        });
      }
      if (m.hasLinkedIdentity) {
        actions.push({
          icon: <UnlinkIcon />,
          label: "Unlink SSO",
          onSelect: () =>
            unlinkWouldStrand(m, isSelf(m))
              ? setDialog({ kind: "unlink-guard" })
              : unlink.mutate({ member: m, self: isSelf(m) }),
        });
      }
    }

    if (m.role === "admin") {
      actions.push({
        icon: <ShieldOffIcon />,
        label: "Remove admin",
        // Demoting the only admin would strand the roster; the backend 409s, but
        // disable it here so the footgun isn't even offered.
        disabled: activeAdmins <= 1,
        onSelect: () => setRole.mutate({ member: m, role: "member" }),
      });
    } else {
      actions.push({
        icon: <ShieldIcon />,
        label: "Make admin",
        onSelect: () => setRole.mutate({ member: m, role: "admin" }),
      });
    }

    actions.push({
      icon: <Trash2Icon />,
      label: "Remove member",
      danger: true,
      disabled: m.role === "admin" && activeAdmins <= 1,
      onSelect: () => setDialog({ kind: "remove", member: m }),
    });

    return inviteBusyMemberID === m.id
      ? actions.map((action) => ({ ...action, disabled: true }))
      : actions;
  };

  return (
    <div className="adm">
      <div className="sec-head mg-rise" style={{ "--i": 0 } as CSSProperties}>
        <div className="sec-title">
          <h2>Roster</h2>
          {!roster.isPending && (
            <span className="sec-count">{plural(active.length, "member")}</span>
          )}
        </div>
        <AddMemberForm
          onCreated={async (memberName, claimUrl) => {
            setDialog({ kind: "invite", name: memberName, claimUrl, purpose: "invite" });
            await reconcileInviteSurfaces(queryClient);
          }}
        />
      </div>

      {roster.isPending ? (
        <p className="adm-state">Loading roster…</p>
      ) : (
        <>
          {invites.isError &&
            active.some(
              (member) =>
                isPlaceholder(member) ||
                member.hasLocalLogin ||
                member.invitePending ||
                inviteByMember.has(member.id),
            ) && (
            <div className="adm-inviteerror" role="status">
              <span>
                {invites.data ? "Invite status may be out of date." : "Couldn't load invite status."}
              </span>
              <button
                type="button"
                className="btn btn--ghost"
                onClick={() => void reconcileInviteSurfaces(queryClient)}
              >
                <RotateCcwIcon aria-hidden="true" />
                Retry
              </button>
            </div>
          )}
          <div className="adm-tablewrap mg-rise" style={{ "--i": 1 } as CSSProperties}>
            <table className="adm-table">
              <thead>
                <tr>
                  <th>Member</th>
                  {COLUMNS.map((c) => (
                    <th key={c.key} className={c.className} data-shed={c.shed || undefined}>
                      {c.header}
                    </th>
                  ))}
                  <th aria-label="Actions" />
                </tr>
              </thead>
              <tbody>
                {active.length === 0 ? (
                  <tr>
                    <td colSpan={COLUMN_COUNT} className="adm-state">
                      No members yet. Add the first one above.
                    </td>
                  </tr>
                ) : (
                  active.map((m) => (
                    <tr key={m.id}>
                      <td>
                        <MemberIdentity member={m} isSelf={isSelf(m)} extras={summaryOf(m)} />
                      </td>
                      {COLUMNS.map((c) => (
                        <td key={c.key} className={c.className} data-shed={c.shed || undefined}>
                          {c.cell(m, inviteStateFor(m))}
                        </td>
                      ))}
                      <td className="adm-rowend">
                        <Menu
                          label={`Actions for ${m.name}`}
                          actions={rowActions(m)}
                          disabled={inviteBusyMemberID === m.id}
                        />
                      </td>
                    </tr>
                  ))
                )}
              </tbody>
            </table>
          </div>

          {archived.length > 0 && (
            <div className="adm-archived mg-rise" style={{ "--i": 2 } as CSSProperties}>
              <div className="adm-archivedhead">
                Archived <span className="adm-count">{archived.length}</span>
                <span className="adm-archivednote">
                  Kept for attribution. Can't log in until restored and re-invited.
                </span>
              </div>
              <div className="adm-tablewrap">
                <table className="adm-table adm-table--dim">
                  <tbody>
                    {archived.map((m) => (
                      <tr key={m.id}>
                        <td>
                          <MemberIdentity member={m} isSelf={isSelf(m)} />
                        </td>
                        {/* Its own table, with its own shape: identity, one summary
                            cell, kebab. Not tied to COLUMNS — an active-table
                            column has no business widening this span. */}
                        <td colSpan={3} className="adm-muted">
                          {credLabel(m)} · {m.moviesAuthored} added
                        </td>
                        <td className="adm-rowend">
                          <Menu label={`Actions for ${m.name}`} actions={rowActions(m)} />
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </div>
          )}
        </>
      )}

      {dialog?.kind === "invite" && (
        <InviteReveal
          name={dialog.name}
          claimUrl={dialog.claimUrl}
          purpose={dialog.purpose}
          onClose={closeDialog}
        />
      )}
      {dialog?.kind === "remove" && (
        <RemoveConfirm
          member={dialog.member}
          pending={removeMember.isPending}
          onConfirm={() => removeMember.mutate(dialog.member)}
          onClose={closeDialog}
        />
      )}
      {dialog?.kind === "unlink-guard" && <UnlinkGuard onClose={closeDialog} />}
      {dialog?.kind === "set-login" && (
        <SetLoginDialog
          member={dialog.member}
          pending={setLogin.isPending}
          onSubmit={(username, password) => setLogin.mutate({ member: dialog.member, username, password })}
          onClose={closeDialog}
        />
      )}
    </div>
  );
}

// The field keeps its own state so a keystroke re-renders the form and nothing
// else. Held in RosterSection it re-rendered every roster row per character, and a
// row is not cheap: an avatar with a layout effect, cred chips, a menu, and a
// freshly built action array.
function AddMemberForm({
  onCreated,
}: {
  onCreated: (name: string, claimUrl: string) => void | Promise<unknown>;
}) {
  const [name, setName] = useState("");

  const createMember = useMutation({
    mutationFn: (memberName: string) => APIClient.members.create(memberName),
    onSuccess: (res, memberName) => {
      setName("");
      return onCreated(memberName, res.claimUrl);
    },
    onError: fail("Couldn't create the member."),
  });

  const handleCreate = (e: FormEvent) => {
    e.preventDefault();
    const trimmed = name.trim();
    if (!trimmed || createMember.isPending) return;
    createMember.mutate(trimmed);
  };

  return (
    <form className="adm-add" onSubmit={handleCreate}>
      <label className="field adm-field">
        <UsersIcon />
        <input
          value={name}
          onChange={(e) => setName(e.target.value)}
          placeholder="New member's name…"
          aria-label="New member name"
        />
      </label>
      <button type="submit" className="btn btn--accent" disabled={!name.trim() || createMember.isPending}>
        <PlusIcon />
        {createMember.isPending ? "Adding…" : "Add & create link"}
      </button>
    </form>
  );
}
