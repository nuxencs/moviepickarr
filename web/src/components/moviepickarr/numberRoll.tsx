import NumberFlow from "@number-flow/react";
import { type ComponentProps, useEffect, useState } from "react";

/* ============================================================
   moviepickarr — shared number-roll primitives (NumberFlow).
   Used by the Stats KPIs/rail headings and the Movies pool /
   watched counts so every animated count rolls identically.
   ============================================================ */

// Number-roll timing — tuned to the MG motion scale (§6): --dur-slow (0.4s) +
// --ease (decelerating ease-out, no bounce). NumberFlow honors
// prefers-reduced-motion itself (renders instantly), matching the global RM
// guard, and only animates on value change (initial paint is static).
const NUMBER_TIMING: EffectTiming = { duration: 400, easing: "cubic-bezier(0.22, 0.61, 0.36, 1)" };

/** A number with the shared MG roll timing. With `animateOnMount` it counts up
 *  from 0 on mount (the KPI strip "wakes up" on each visit); otherwise it's
 *  static on first paint and only rolls on value change. */
export function StatNumber({
  animateOnMount = false,
  ...props
}: ComponentProps<typeof NumberFlow> & { animateOnMount?: boolean }) {
  if (animateOnMount) return <MountRollNumber {...props} />;
  return <NumberFlow transformTiming={NUMBER_TIMING} spinTiming={NUMBER_TIMING} {...props} />;
}

/** Renders 0 first, then bumps to the real value after mount so NumberFlow rolls
 *  0 -> value. Reduced motion collapses it to an instant set (NumberFlow's default). */
export function MountRollNumber({ value, ...props }: ComponentProps<typeof NumberFlow>) {
  const [display, setDisplay] = useState(0);
  useEffect(() => {
    setDisplay(value);
  }, [value]);
  return <NumberFlow transformTiming={NUMBER_TIMING} spinTiming={NUMBER_TIMING} value={display} {...props} />;
}
