// Pure presentation logic for the account page's device list. Kept out of the
// component so the copy rules (what the second line says, which rows offer a
// sign-out) are unit-tested without rendering, the way account.ts is.
import type { SessionSummary } from "@/types/Response";

import { timeAgo } from "@/lib/time";


/**
 * The muted second line under a device: when it was last active, then the IP it
 * was last seen from. Either half can be missing (a session with no recorded IP,
 * an unparseable timestamp), so the parts are joined rather than templated.
 */
export function sessionMeta(s: SessionSummary, now: number = Date.now()): string {
  const parts: string[] = [];
  const active = timeAgo(s.lastSeenAt, now);
  if (active) parts.push(active === "now" ? "active now" : `active ${active}`);
  if (s.ip) parts.push(s.ip);
  return parts.join(" · ");
}

/**
 * How many devices a log-out-everywhere would close besides this one. Derived
 * from the loaded list rather than carried on /me: two sources for one number
 * can disagree, and the list is the one the member is looking at.
 */
export function otherDeviceCount(sessions: SessionSummary[]): number {
  return sessions.filter((s) => !s.current).length;
}
