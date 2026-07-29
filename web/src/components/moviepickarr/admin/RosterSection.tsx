import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import {
  KeyRoundIcon,
  MailPlusIcon,
  PlusIcon,
  RotateCcwIcon,
  ShieldIcon,
  ShieldOffIcon,
  Trash2Icon,
  UnlinkIcon,
  UsersIcon,
} from "lucide-react";
import { FormEvent, useState, type CSSProperties, type ReactNode } from "react";

import { APIClient, ApiError } from "@/api/APIClient";
import { MeQueryOptions, RosterQueryOptions } from "@/api/queries";
import { UsersKeys } from "@/api/query_keys";

import { credLabel, isPlaceholder, timeAgo, unlinkWouldStrand } from "@/components/moviepickarr/admin/roster";
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


import type { RosterMember } from "@/types/Response";

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
  cell: (m: RosterMember) => ReactNode;
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
    cell: (m) => <CredChips member={m} />,
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
  | { kind: "invite"; name: string; claimUrl: string }
  | { kind: "remove"; member: RosterMember }
  | { kind: "unlink-guard" }
  | { kind: "set-login"; member: RosterMember }
  | null;

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

  const [dialog, setDialog] = useState<Dialog>(null);

  const refresh = () => queryClient.invalidateQueries({ queryKey: UsersKeys.roster() });
  const closeDialog = () => setDialog(null);

  // Each mutation refetches the roster on success so the surface reflects the
  // new state immediately.
  const reissueInvite = useMutation({
    mutationFn: (member: RosterMember) => APIClient.members.reissueInvite(member.id),
    onSuccess: (res, member) => {
      refresh();
      setDialog({ kind: "invite", name: member.name, claimUrl: res.claimUrl });
    },
    onError: fail("Couldn't regenerate the invite."),
  });

  const revokeInvite = useMutation({
    mutationFn: (member: RosterMember) => APIClient.members.revokeInvite(member.id),
    onSuccess: (_res, member) => {
      refresh();
      toast.success(`Invite for ${member.name} revoked`);
    },
    onError: fail("Couldn't revoke the invite."),
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
      refresh();
      toast.success(`Password ${member.hasLocalLogin ? "reset" : "set"} for ${member.name}`);
    },
    onError: fail("Couldn't save the password."),
  });

  const removeLogin = useMutation({
    mutationFn: (member: RosterMember) => APIClient.members.removeLocalLogin(member.id),
    onSuccess: (_res, member) => {
      refresh();
      toast.success(`Password removed for ${member.name}`);
    },
    onError: fail("Couldn't remove the password."),
  });

  const unlink = useMutation({
    mutationFn: ({ member, self }: { member: RosterMember; self: boolean }) =>
      self ? APIClient.members.unlinkSelf() : APIClient.members.unlink(member.id),
    onSuccess: (_res, { member, self }) => {
      refresh();
      if (self) queryClient.invalidateQueries({ queryKey: ["auth", "me"] });
      toast.success(`SSO unlinked for ${member.name}`);
    },
    onError: fail("Couldn't unlink SSO."),
  });

  const removeMember = useMutation({
    mutationFn: (member: RosterMember) => APIClient.members.remove(member.id),
    onSuccess: (res, member) => {
      closeDialog();
      refresh();
      toast.success(res.outcome === "deleted" ? `${member.name} deleted` : `${member.name} archived`);
    },
    onError: fail("Couldn't remove the member."),
  });

  const restore = useMutation({
    mutationFn: (member: RosterMember) => APIClient.members.restore(member.id),
    onSuccess: (res, member) => {
      refresh();
      setDialog({ kind: "invite", name: member.name, claimUrl: res.claimUrl });
    },
    onError: fail("Couldn't restore the member."),
  });

  // A non-admin gets 403 from the roster read: render the first-class forbidden
  // state, never a 404 mask. Any other error is a genuine load failure.
  if (roster.isError) {
    if (roster.error instanceof ApiError && roster.error.status === 403) {
      return <ForbiddenState onLeave={() => navigate({ to: "/" })} />;
    }
    return <p className="adm-state">Couldn't load the roster. Try again in a moment.</p>;
  }

  const members = roster.data ?? [];
  const active = members.filter((m) => !m.archived);
  const archived = members.filter((m) => m.archived);
  const activeAdmins = active.filter((m) => m.role === "admin").length;
  const isSelf = (m: RosterMember) => me?.id === m.id;

  // The row kebab: contextual per credential state. Placeholders get invite
  // actions; credentialed members get password + unlink; every active member can
  // be promoted/demoted and removed. Archived members can only be restored.
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

    if (isPlaceholder(m)) {
      actions.push({
        icon: <MailPlusIcon />,
        label: m.invitePending ? "Regenerate invite" : "Send invite",
        onSelect: () => reissueInvite.mutate(m),
      });
      if (m.invitePending) {
        actions.push({
          icon: <Trash2Icon />,
          label: "Revoke invite",
          onSelect: () => revokeInvite.mutate(m),
        });
      }
    } else {
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
      onSelect: () => setDialog({ kind: "remove", member: m }),
    });

    return actions;
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
          onCreated={(memberName, claimUrl) => {
            refresh();
            setDialog({ kind: "invite", name: memberName, claimUrl });
          }}
        />
      </div>

      {roster.isPending ? (
        <p className="adm-state">Loading roster…</p>
      ) : (
        <>
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
                          {c.cell(m)}
                        </td>
                      ))}
                      <td className="adm-rowend">
                        <Menu label={`Actions for ${m.name}`} actions={rowActions(m)} />
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
        <InviteReveal name={dialog.name} claimUrl={dialog.claimUrl} onClose={closeDialog} />
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
function AddMemberForm({ onCreated }: { onCreated: (name: string, claimUrl: string) => void }) {
  const [name, setName] = useState("");

  const createMember = useMutation({
    mutationFn: (memberName: string) => APIClient.members.create(memberName),
    onSuccess: (res, memberName) => {
      setName("");
      onCreated(memberName, res.claimUrl);
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
        Add &amp; invite
      </button>
    </form>
  );
}
