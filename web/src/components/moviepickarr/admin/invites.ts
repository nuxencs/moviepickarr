// Invite timing copy for the admin roster. Pure derivations stay outside the
// component so clock behavior can be tested without a DOM.

import type { InviteStatus, InviteSummary } from "@/types/Response";

import { timeAgo, timeUntil } from "@/lib/time";

/**
 * The row's expiry line. Open reads forward ("expires in 3 days"), expired
 * reads back ("expired 2 days ago"). Both edges are worded rather than left to
 * a bare number: a link lapsing inside the minute says "expires shortly", and
 * one that just lapsed says "expired just now", because "in 0 minutes" and
 * "expired now" both read as broken rather than imminent.
 */
export function expiryLabel(
  invite: InviteSummary,
  now: number = Date.now(),
  status: InviteStatus = inviteStatusAt(invite, now),
): string {
  if (status === "open") {
    const left = timeUntil(invite.expiresAt, now);
    return left ? `expires in ${left}` : "expires shortly";
  }
  const since = timeAgo(invite.expiresAt, now);
  return since && since !== "now" ? `expired ${since}` : "expired just now";
}

/** Convert client elapsed time into the server's clock domain. dataUpdatedAt is
 * when React Query accepted the response, so local wall-clock skew drops out. */
export function serverAlignedNow(
  serverNow: string,
  dataUpdatedAt: number,
  clientNow: number = Date.now(),
): number {
  return Date.parse(serverNow) + Math.max(0, clientNow - dataUpdatedAt);
}

/** Exact expiry boundary: redeemability is strict serverNow < expiresAt. */
export function inviteStatusAt(invite: InviteSummary, now: number): InviteStatus {
  return now < Date.parse(invite.expiresAt) ? "open" : "expired";
}

/** Delay until the next current invite crosses into expired. */
export function nextInviteExpiryDelay(invites: InviteSummary[], now: number): number | null {
  const expiries = invites
    .filter((invite) => inviteStatusAt(invite, now) === "open")
    .map((invite) => Date.parse(invite.expiresAt) - now);
  return expiries.length > 0 ? Math.max(0, Math.min(...expiries)) : null;
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
