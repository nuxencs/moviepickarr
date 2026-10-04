import type { SessionSummary } from "@/types/Response";

import { timeAgo } from "@/lib/time";


/** The "active 2h ago" line; empty for an unparseable timestamp. */
export function sessionMeta(s: SessionSummary, now: number = Date.now()): string {
  const active = timeAgo(s.lastSeenAt, now);
  if (!active) return "";
  return active === "now" ? "active now" : `active ${active}`;
}

/** From the loaded list, not /me, so it matches what the member sees. */
export function otherDeviceCount(sessions: SessionSummary[]): number {
  return sessions.filter((s) => !s.current).length;
}
