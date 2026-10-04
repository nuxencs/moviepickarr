/** How long a closing overlay stays mounted for its exit animation; reads `--dur-fast` to stay in step with CSS. */
export function exitDelayMs(): number {
  if (window.matchMedia("(prefers-reduced-motion: reduce)").matches) return 0;
  const raw = getComputedStyle(document.documentElement).getPropertyValue("--dur-fast");
  const secs = parseFloat(raw) || 0.14;
  return Math.round(secs * 1000) + 20;
}
