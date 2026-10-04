const UNITS: [limit: number, secs: number, name: string][] = [
  [60, 1, "second"],
  [3600, 60, "minute"],
  [86400, 3600, "hour"],
  [604800, 86400, "day"],
  [2629800, 604800, "week"],
  [31557600, 2629800, "month"],
  [Infinity, 31557600, "year"],
];

/** The largest whole unit for a span in seconds ("3 days"), with no direction word. */
function span(secs: number): string {
  for (const [limit, per, name] of UNITS) {
    if (secs < limit) {
      const n = Math.max(1, Math.floor(secs / per));
      return `${n} ${name}${n === 1 ? "" : "s"}`;
    }
  }
  return "";
}

/** A compact "3 days ago". Returns "" for a missing or unparseable timestamp. */
export function timeAgo(iso: string | undefined, now: number = Date.now()): string {
  if (!iso) return "";
  const then = Date.parse(iso);
  if (Number.isNaN(then)) return "";
  const secs = Math.max(0, Math.round((now - then) / 1000));
  if (secs < 45) return "now";
  const rest = span(secs);
  return rest ? `${rest} ago` : "";
}

/**
 * Time left until a timestamp ("3 days"), with no direction word. Returns "" for
 * a missing, unparseable, past or under-a-minute one, not "0 minutes".
 */
export function timeUntil(iso: string | undefined, now: number = Date.now()): string {
  if (!iso) return "";
  const then = Date.parse(iso);
  if (Number.isNaN(then)) return "";
  const secs = Math.round((then - now) / 1000);
  if (secs < 45) return "";
  return span(secs);
}
