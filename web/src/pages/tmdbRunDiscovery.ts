const MAX_BROWSER_TIMEOUT_MS = 2_147_483_647;
const DUE_TIME_BUFFER_MS = 100;

/** Returns null once the check is due; long waits are split into browser-safe chunks. */
export function scheduledRunDiscoveryDelay(dueAt: number, now: number): number | null {
  const remaining = dueAt - now;
  if (remaining <= 0) return null;
  return Math.min(remaining + DUE_TIME_BUFFER_MS, MAX_BROWSER_TIMEOUT_MS);
}
