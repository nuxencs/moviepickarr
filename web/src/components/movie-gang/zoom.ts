/**
 * The effective CSS zoom applied to `el`. The large-screen scale ramp (§13 in
 * index.css) lives on `:root` and cascades into everything below it, so any JS
 * that positions or translates an element from getBoundingClientRect()
 * coordinates — which are already in the zoomed viewport space — must divide by
 * this factor to land on target. Prefers the element's own `currentCSSZoom`
 * (the cumulative zoom the engine actually rendered it at); falls back to the
 * declared `:root` zoom on engines without the property, where an element's own
 * computed zoom reads as 1 and would be a no-op.
 */
export function effectiveZoom(el: Element): number {
  const cur = (el as { currentCSSZoom?: number }).currentCSSZoom;
  if (typeof cur === "number" && cur > 0) return cur;
  const z = parseFloat(getComputedStyle(document.documentElement).zoom || "1");
  return Number.isFinite(z) && z > 0 ? z : 1;
}
