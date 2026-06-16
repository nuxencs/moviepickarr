/**
 * How long to keep a closing floating surface mounted so its exit animation can
 * finish, read from the shared `--dur-fast` token so CSS and JS never desync.
 * Reduced-motion users skip the wait entirely. Shared by the `Modal` and the
 * bespoke `Menu` so every overlay's unmount delay stays in lockstep with the CSS.
 */
export function exitDelayMs(): number {
  if (window.matchMedia("(prefers-reduced-motion: reduce)").matches) return 0;
  const raw = getComputedStyle(document.documentElement).getPropertyValue("--dur-fast");
  const secs = parseFloat(raw) || 0.14;
  return Math.round(secs * 1000) + 20;
}
