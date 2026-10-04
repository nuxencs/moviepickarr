import { useCallback, useState } from "react";

/** Boolean state plus a flipper with a stable identity. */
export function useToggle(initialValue = false): [boolean, () => void] {
    const [value, setValue] = useState(initialValue);
    const toggle = useCallback(() => setValue(v => !v), []);

    return [value, toggle];
}
