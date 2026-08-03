// Time formatting shared across surfaces. Lives here rather than beside one of
// them because two unrelated features render the same "when did this last
// happen" line: the admin roster's last-active column and the account page's
// session list.

const UNITS: [limit: number, secs: number, name: string][] = [
  [60, 1, "second"],
  [3600, 60, "minute"],
  [86400, 3600, "hour"],
  [604800, 86400, "day"],
  [2629800, 604800, "week"],
  [31557600, 2629800, "month"],
  [Infinity, 31557600, "year"],
];

/**
 * A compact "time ago" for a timestamp. Returns "" for a missing or unparseable
 * one (a member who never logged in) so the caller can render a dash. "now"
 * under a minute, otherwise the largest whole unit ("3 days ago").
 */
export function timeAgo(iso: string | undefined, now: number = Date.now()): string {
  if (!iso) return "";
  const then = Date.parse(iso);
  if (Number.isNaN(then)) return "";
  const secs = Math.max(0, Math.round((now - then) / 1000));
  if (secs < 45) return "now";
  for (const [limit, per, name] of UNITS) {
    if (secs < limit) {
      const n = Math.max(1, Math.floor(secs / per));
      return `${n} ${name}${n === 1 ? "" : "s"} ago`;
    }
  }
  return "";
}
