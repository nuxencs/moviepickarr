export const AuthKeys = {
    all: ["auth"] as const,
    me: () => [...AuthKeys.all, "me"] as const,
    config: () => [...AuthKeys.all, "config"] as const,
    claim: (token: string) => [...AuthKeys.all, "claim", token] as const,
}

export const UsersKeys = {
    all: ["users"] as const,
    list: () => [...UsersKeys.all, "list"] as const,
    pool: () => [...UsersKeys.all, "pool"] as const,
    stash: () => [...UsersKeys.all, "stash"] as const,
    // The admin roster (presence-derived login state per member). Distinct from
    // list() (the movie-board members), but rides the same "users" root so a
    // roster mutation and a board change stale under one prefix.
    roster: () => [...UsersKeys.all, "roster"] as const,
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
    // The filter segment arrives pre-canonicalized (comma-joined sorted id
    // lists) from StatsGetQueryOptions' one serializer, so selection order
    // can't split the cache and the key always matches the request sent.
    byWindow: (
        window: string,
        timezone: string,
        start: string | undefined,
        end: string | undefined,
        f: { genre?: string; actorIds?: string; crewIds?: string; addedByIds?: string; releaseYear?: number; decade?: number },
    ) =>
      [...StatsKeys.all, "window", window, "tz", timezone, "start", start ?? "", "end", end ?? "", "genre", f.genre ?? "", "actors", f.actorIds ?? "", "crew", f.crewIds ?? "", "addedBy", f.addedByIds ?? "", "releaseYear", f.releaseYear ?? 0, "decade", f.decade ?? 0] as const,
}
