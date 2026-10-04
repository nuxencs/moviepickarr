import type { InviteStatus, InviteSummary } from "@/types/Response";

import { timeAgo, timeUntil } from "@/lib/time";

/** Edges are worded ("expires shortly", "expired just now"): "in 0 minutes" reads as broken. */
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

/** Server-clock now: dataUpdatedAt anchors elapsed time, so client clock skew drops out. */
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

export function nextInviteExpiryDelay(invites: InviteSummary[], now: number): number | null {
  const expiries = invites
    .filter((invite) => inviteStatusAt(invite, now) === "open")
    .map((invite) => Date.parse(invite.expiresAt) - now);
  return expiries.length > 0 ? Math.max(0, Math.min(...expiries)) : null;
}

/** "issued by Ada · 2 days ago", or null when the issuer is unknown (seeded or deleted). */
export function issuedLabel(invite: InviteSummary, now: number = Date.now()): string | null {
  if (!invite.issuedBy) return null;
  const when = timeAgo(invite.issuedAt, now);
  return when ? `issued by ${invite.issuedBy} · ${when}` : `issued by ${invite.issuedBy}`;
}
