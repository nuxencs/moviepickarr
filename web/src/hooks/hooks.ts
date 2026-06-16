import { type RefObject, useLayoutEffect, useRef, useState } from "react";

export function useToggle(initialValue = false): [boolean, () => void] {
    const [value, setValue] = useState(initialValue);
    const toggle = () => setValue(v => !v);

    return [value, toggle];
}

/**
 * Replays a CSS entrance animation on the returned ref's subtree whenever
 * `fingerprint` changes — WITHOUT remounting, so NumberFlow counts (or any other
 * stateful children) inside keep rolling rather than snapping. The CSS gates the
 * entrance behind a `data-animate` attribute; on a genuine change we strip it,
 * force a synchronous reflow, then re-add it — the canonical CSS-animation
 * restart. Runs in a layout effect so the strip/restart lands before paint (no
 * flash of the settled state).
 *
 * `fingerprint` MUST be derived from content (e.g. a join of ids/counts), never
 * from render identity — so unrelated re-renders and SSE refetches that resolve
 * to the same data don't re-fire the animation. First mount is a no-op here (the
 * attribute is already present in JSX, so the entrance plays naturally); only
 * subsequent changes trigger the restart.
 *
 * prefers-reduced-motion is honored for free by the global CSS guard, since this
 * only ever drives CSS `animation`/`animation-delay`.
 */
export function useReplayOnChange<T extends HTMLElement>(fingerprint: string): RefObject<T | null> {
    const ref = useRef<T>(null);
    const prev = useRef<string | null>(null);

    useLayoutEffect(() => {
        // First mount: record the baseline and let the JSX-set attribute animate
        // naturally; don't restart (that would double-fire on open).
        if (prev.current === null || prev.current === fingerprint) {
            prev.current = fingerprint;
            return;
        }
        prev.current = fingerprint;
        const el = ref.current;
        if (!el) return;
        el.removeAttribute("data-animate");
        void el.offsetWidth; // force reflow so re-adding the attribute restarts the animation
        el.setAttribute("data-animate", "");
    }, [fingerprint]);

    return ref;
}
