import { useCallback, useEffect, useLayoutEffect, useRef, useState } from "react";

export interface SlidingTabIndicatorPosition {
  left: number;
  width: number;
}

/** One measured underline that stays mounted and moves between navigation links. */
export function useSlidingTabIndicator<Key extends string>(
  active: Key | null | undefined,
  itemCount: number,
  inset = 12,
) {
  const itemRefs = useRef(new Map<Key, HTMLElement>());
  const [position, setPosition] = useState<SlidingTabIndicatorPosition | null>(null);

  const setItemRef = useCallback((key: Key, element: HTMLElement | null) => {
    if (element) itemRefs.current.set(key, element);
    else itemRefs.current.delete(key);
  }, []);

  const measure = useCallback(() => {
    if (!active) {
      setPosition(null);
      return;
    }
    const item = itemRefs.current.get(active);
    if (!item || item.offsetParent === null) return;
    const next = {
      left: item.offsetLeft + inset,
      width: Math.max(0, item.offsetWidth - inset * 2),
    };
    setPosition((current) =>
      current?.left === next.left && current.width === next.width ? current : next,
    );
  }, [active, inset]);

  useLayoutEffect(() => measure(), [itemCount, measure]);

  useEffect(() => {
    let cancelled = false;
    window.addEventListener("resize", measure);
    document.fonts?.ready.then(() => {
      if (!cancelled) measure();
    });
    return () => {
      cancelled = true;
      window.removeEventListener("resize", measure);
    };
  }, [measure]);

  return { position, setItemRef };
}
