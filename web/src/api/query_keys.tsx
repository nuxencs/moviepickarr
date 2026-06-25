export const UsersKeys = {
    all: ["users"] as const,
    list: () => [...UsersKeys.all, "list"] as const,
    pool: () => [...UsersKeys.all, "pool"] as const,
    stash: () => [...UsersKeys.all, "stash"] as const,
}

export const MoviesKeys = {
    all: ["movies"] as const,
    listpool: () => [...MoviesKeys.all, "listpool"] as const,
    current: () => [...MoviesKeys.all, "current"] as const,
    listwatched: () => [...MoviesKeys.all, "listwatched"] as const,
}

// Client-only state (no endpoint): the in-flight pick-reveal spin descriptor,
// set via setQueryData by the SSE handler / pick mutation and read by the Hero.
// `revealed` is the cross-client close signal — the picker's confirm (or the
// countdown) broadcasts movie:revealed; useSSE stows the pickedAt here and the
// Hero closes the reel for that pick.
export const PickKeys = {
    all: ["pick"] as const,
    active: () => [...PickKeys.all, "active"] as const,
    revealed: () => [...PickKeys.all, "revealed"] as const,
}

export const SettingsKeys = {
    all: ["settings"] as const,
    poolLock: () => [...SettingsKeys.all, "poolLock"] as const,
    nextPicker: () => [...SettingsKeys.all, "nextPicker"] as const,
}

export const StatsKeys = {
    all: ["stats"] as const,
    // actorsKey/crewKey are the canonical comma-joined id lists (sorted in
    // StatsGetQueryOptions), so selection order can't split the cache.
    byWindow: (window: string, timezone: string, start?: string, end?: string, genre?: string, actorsKey?: string, crewKey?: string, addedByKey?: string, releaseYear?: number, decade?: number) =>
      [...StatsKeys.all, "window", window, "tz", timezone, "start", start ?? "", "end", end ?? "", "genre", genre ?? "", "actors", actorsKey ?? "", "crew", crewKey ?? "", "addedBy", addedByKey ?? "", "releaseYear", releaseYear ?? 0, "decade", decade ?? 0] as const,
}
