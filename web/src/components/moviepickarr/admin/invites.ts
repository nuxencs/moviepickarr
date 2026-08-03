// Row copy and grouping for the admin invites section. Pure derivations, kept
// out of the component for the same reason roster.ts is: the wording of an
// expiry and the split into Open/Expired are the parts worth testing directly,
// and neither needs a DOM to be wrong.

import type { InviteSummary } from "@/types/Response";

import { timeAgo, timeUntil } from "@/lib/time";

/**
 * The row's expiry line. Open reads forward ("expires in 3 days"), expired
 * reads back ("expired 2 days ago"). Both edges are worded rather than left to
 * a bare number: a link lapsing inside the minute says "expires shortly", and
 * one that just lapsed says "expired just now", because "in 0 minutes" and
 * "expired now" both read as broken rather than imminent.
 */
export function expiryLabel(invite: InviteSummary, now: number = Date.now()): string {
  if (invite.status === "open") {
    const left = timeUntil(invite.expiresAt, now);
    return left ? `expires in ${left}` : "expires shortly";
  }
  const since = timeAgo(invite.expiresAt, now);
  return since && since !== "now" ? `expired ${since}` : "expired just now";
}

/**
 * "issued by Ada · 2 days ago", or null when the invite records no issuer.
 * created_by is nullable (a seeded invite, or an issuing admin since deleted),
 * and the whole line is dropped rather than crediting a "System" that never
 * issued anything.
 */
export function issuedLabel(invite: InviteSummary, now: number = Date.now()): string | null {
  if (!invite.issuedBy) return null;
  const when = timeAgo(invite.issuedAt, now);
  return when ? `issued by ${invite.issuedBy} · ${when}` : `issued by ${invite.issuedBy}`;
}

/**
 * The two groups the section renders. Order inside each is the server's (open
 * soonest-to-lapse first, expired most-recently-lapsed first), so the row
 * nearest needing attention leads its group and the client never re-sorts on a
 * clock the server didn't use.
 */
export function groupInvites(invites: InviteSummary[]): {
  open: InviteSummary[];
  expired: InviteSummary[];
} {
  return {
    open: invites.filter((i) => i.status === "open"),
    expired: invites.filter((i) => i.status === "expired"),
  };
}
