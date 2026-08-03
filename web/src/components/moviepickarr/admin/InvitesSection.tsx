import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { EyeOffIcon, MailPlusIcon, RotateCwIcon, Trash2Icon } from "lucide-react";
import { useState, type CSSProperties } from "react";

import { APIClient, ApiError } from "@/api/APIClient";
import { InvitesQueryOptions } from "@/api/queries";
import { InvitesKeys, UsersKeys } from "@/api/query_keys";

import { expiryLabel, groupInvites, issuedLabel } from "@/components/moviepickarr/admin/invites";
import { InviteReveal } from "@/components/moviepickarr/admin/RosterOverlays";
import { Avatar } from "@/components/moviepickarr/Bits";
import { Menu, type MenuAction } from "@/components/moviepickarr/Menu";
import { toast } from "@/components/ui/toast-api";

import type { InviteSummary } from "@/types/Response";

import "@/components/moviepickarr/admin/invites.css";

const fail = (fallback: string) => (err: unknown) =>
  toast.error(err instanceof ApiError && err.message ? err.message : fallback);

/**
 * Who is still waiting to set up a login, above the roster. Its own section
 * rather than a roster column: the roster is one row per member and says only
 * whether an invite is pending, while this is one row per outstanding invite
 * with the two things an admin acts on, how long it has left and who sent it.
 *
 * It is absent, not empty, when nobody is waiting. That is the normal state of
 * a settled instance, and a permanent "No pending invites" panel would be a
 * section that exists to say it has nothing to say.
 *
 * One rule shapes every action here: only the invite's SHA-256 is stored, so an
 * existing link cannot be shown again by anyone, ever. Regenerate and Re-send
 * both mint a fresh invite (killing the old one) and reveal it once through the
 * same ceremony the roster uses. Nothing here can offer to copy a link again.
 */
export function InvitesSection() {
  const queryClient = useQueryClient();
  const invites = useQuery(InvitesQueryOptions());

  // The one-time claim URL from a regenerate/re-send. Its own state rather than
  // the roster's dialog union: this section owns exactly one ceremony.
  const [revealed, setRevealed] = useState<{ name: string; claimUrl: string } | null>(null);

  // Every mutation here changes both surfaces on the page: the invite rows, and
  // the roster's invitePending chip, which reads the same fact.
  const refresh = () => {
    void queryClient.invalidateQueries({ queryKey: InvitesKeys.all });
    void queryClient.invalidateQueries({ queryKey: UsersKeys.roster() });
  };

  const reissue = useMutation({
    mutationFn: (invite: InviteSummary) => APIClient.members.reissueInvite(invite.memberId),
    onSuccess: (res, invite) => {
      refresh();
      setRevealed({ name: invite.memberName, claimUrl: res.claimUrl });
    },
    onError: fail("Couldn't issue a new invite."),
  });

  const revoke = useMutation({
    mutationFn: (invite: InviteSummary) => APIClient.members.revokeInvite(invite.memberId),
    onSuccess: (_res, invite) => {
      refresh();
      toast.success(`Invite for ${invite.memberName} revoked`);
    },
    onError: fail("Couldn't revoke the invite."),
  });

  const dismiss = useMutation({
    mutationFn: (invite: InviteSummary) => APIClient.invites.dismiss(invite.id),
    onSuccess: (_res, invite) => {
      refresh();
      toast.success(`Dismissed ${invite.memberName}'s expired invite`);
    },
    // A 404 means the row went while the admin was looking at it (claimed,
    // regenerated in another tab): the list they clicked was stale, so refresh
    // and say what happened rather than reporting a failure they can't act on.
    onError: (err, invite) => {
      refresh();
      if (err instanceof ApiError && err.status === 404) {
        toast.success(`${invite.memberName}'s invite was already gone`);
        return;
      }
      fail("Couldn't dismiss the invite.")(err);
    },
  });

  // A non-admin gets 403 from this read too, but the roster below renders the
  // page's one forbidden state; a second copy of it above would just repeat
  // itself. Any other failure gets a line, because a section that silently
  // vanishes on error is indistinguishable from one with nothing to show.
  if (invites.isError) {
    if (invites.error instanceof ApiError && invites.error.status === 403) return null;
    return (
      <div className="inv">
        <p className="inv-state">Couldn't load invites. Try again in a moment.</p>
      </div>
    );
  }

  // No loading state on purpose: the section's usual answer is "nothing", so a
  // placeholder would flash a section that then isn't there.
  const rows = invites.data ?? [];
  if (rows.length === 0) return null;

  const { open, expired } = groupInvites(rows);

  const actionsFor = (invite: InviteSummary): MenuAction[] =>
    invite.status === "open"
      ? [
          {
            icon: <RotateCwIcon />,
            // Not "copy the link again": the old one is unrecoverable, so this
            // mints a replacement and retires the outstanding invite.
            label: "Regenerate invite",
            onSelect: () => reissue.mutate(invite),
          },
          {
            icon: <Trash2Icon />,
            label: "Revoke invite",
            onSelect: () => revoke.mutate(invite),
          },
        ]
      : [
          {
            icon: <MailPlusIcon />,
            label: "Re-send invite",
            onSelect: () => reissue.mutate(invite),
          },
          {
            icon: <EyeOffIcon />,
            label: "Dismiss invite",
            onSelect: () => dismiss.mutate(invite),
          },
        ];

  const group = (title: string, invitesInGroup: InviteSummary[]) =>
    invitesInGroup.length > 0 && (
      <section className="inv-group">
        <h3 className="inv-grouphead">
          {title} <span className="inv-count">{invitesInGroup.length}</span>
        </h3>
        <ul className="inv-list">
          {invitesInGroup.map((invite) => {
            const issued = issuedLabel(invite);
            return (
              <li key={invite.id} className="inv-row">
                <Avatar name={invite.memberName} size={34} />
                <div className="inv-rowtext">
                  <div className="inv-rowname">{invite.memberName}</div>
                  <div className="inv-rowmeta">
                    <span>{expiryLabel(invite)}</span>
                    {issued && <span className="inv-issued">{issued}</span>}
                  </div>
                </div>
                <Menu label={`Actions for ${invite.memberName}'s invite`} actions={actionsFor(invite)} />
              </li>
            );
          })}
        </ul>
      </section>
    );

  // The entrance cascades within the section (head, then rows), not across the
  // page: this section is conditional, so pushing the roster's `--i` up to sit
  // after it would delay the roster on every instance that has no invites,
  // which is most of them.
  return (
    <div className="inv">
      <div className="sec-head mg-rise" style={{ "--i": 0 } as CSSProperties}>
        <div className="sec-title">
          <h2>Invites</h2>
        </div>
      </div>

      <div className="inv-groups mg-rise" style={{ "--i": 1 } as CSSProperties}>
        {group("Open", open)}
        {group("Expired", expired)}
      </div>

      {revealed && (
        <InviteReveal
          name={revealed.name}
          claimUrl={revealed.claimUrl}
          onClose={() => setRevealed(null)}
        />
      )}
    </div>
  );
}
