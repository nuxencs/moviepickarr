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
    // Full enriched record (cast/crew/overview) lazy-loaded by the detail modal,
    // so the list payloads can ship lean. `details()` is the prefix used to
    // invalidate every open/cached modal on enrichment.
    details: () => [...MoviesKeys.all, "detail"] as const,
    detail: (movieID: number) => [...MoviesKeys.details(), movieID] as const,
    // Stats filter choices (genres/actors/crew/years/adders), derived
    // server-side from the watched library — replaces deriving them from the
    // credits that used to be embedded in the watched list.
    filterOptions: () => [...MoviesKeys.all, "filterOptions"] as const,
}

export const SettingsKeys = {
    all: ["settings"] as const,
    poolLock: () => [...SettingsKeys.all, "poolLock"] as const,
    nextUp: () => [...SettingsKeys.all, "nextUp"] as const,
}

export const StatsKeys = {
    all: ["stats"] as const,
    // actorsKey/crewKey are the canonical comma-joined id lists (sorted in
    // StatsGetQueryOptions), so selection order can't split the cache.
    byWindow: (window: string, timezone: string, start?: string, end?: string, genre?: string, actorsKey?: string, crewKey?: string, addedByKey?: string, releaseYear?: number, decade?: number) =>
      [...StatsKeys.all, "window", window, "tz", timezone, "start", start ?? "", "end", end ?? "", "genre", genre ?? "", "actors", actorsKey ?? "", "crew", crewKey ?? "", "addedBy", addedByKey ?? "", "releaseYear", releaseYear ?? 0, "decade", decade ?? 0] as const,
}
