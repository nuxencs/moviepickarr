import { useCallback, useState } from "react";

/** Boolean state plus a flipper. The flipper keeps a stable identity across
 *  renders (functional update, no dep on the value), so passing it to a
 *  memoized child doesn't re-render that child on every parent render. */
export function useToggle(initialValue = false): [boolean, () => void] {
    const [value, setValue] = useState(initialValue);
    const toggle = useCallback(() => setValue(v => !v), []);

    return [value, toggle];
}
