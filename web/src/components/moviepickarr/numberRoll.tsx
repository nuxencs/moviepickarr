import NumberFlow from "@number-flow/react";
import { type ComponentProps, useEffect, useState } from "react";

// Mirrors --dur-slow and --ease (DESIGN.md §6). NumberFlow honors reduced motion itself.
const NUMBER_TIMING: EffectTiming = { duration: 400, easing: "cubic-bezier(0.22, 0.61, 0.36, 1)" };

/** A number with the shared roll timing. `animateOnMount` counts up from 0; otherwise static on first paint. */
export function StatNumber({
  animateOnMount = false,
  ...props
}: ComponentProps<typeof NumberFlow> & { animateOnMount?: boolean }) {
  if (animateOnMount) return <MountRollNumber {...props} />;
  return <NumberFlow transformTiming={NUMBER_TIMING} spinTiming={NUMBER_TIMING} {...props} />;
}

/** Renders 0, then the real value after mount, so NumberFlow rolls 0 -> value. */
export function MountRollNumber({ value, ...props }: ComponentProps<typeof NumberFlow>) {
  const [display, setDisplay] = useState(0);
  useEffect(() => {
    setDisplay(value);
  }, [value]);
  return <NumberFlow transformTiming={NUMBER_TIMING} spinTiming={NUMBER_TIMING} value={display} {...props} />;
}
