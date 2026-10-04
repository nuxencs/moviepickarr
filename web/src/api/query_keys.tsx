export const AuthKeys = {
    all: ["auth"] as const,
    me: () => [...AuthKeys.all, "me"] as const,
    config: () => [...AuthKeys.all, "config"] as const,
    posterWall: () => [...AuthKeys.all, "poster-wall"] as const,
    claim: (token: string) => [...AuthKeys.all, "claim", token] as const,
    // Under "auth" so an identity change stales the device list too.
    sessions: () => [...AuthKeys.all, "sessions"] as const,
}

export const UsersKeys = {
    all: ["users"] as const,
    list: () => [...UsersKeys.all, "list"] as const,
    pool: () => [...UsersKeys.all, "pool"] as const,
    stash: () => [...UsersKeys.all, "stash"] as const,
    // Admin roster, distinct from list() but under "users" so both stale together.
    roster: () => [...UsersKeys.all, "roster"] as const,
}

// Own root, not under "users": invite changes must stale this and the roster
// explicitly (see reconcileInviteSurfaces).
export const InvitesKeys = {
    all: ["invites"] as const,
    list: () => [...InvitesKeys.all, "list"] as const,
}

export const MoviesKeys = {
    all: ["movies"] as const,
    listpool: () => [...MoviesKeys.all, "listpool"] as const,
    current: () => [...MoviesKeys.all, "current"] as const,
    wildcard: () => [...MoviesKeys.all, "wildcard"] as const,
    listwatched: () => [...MoviesKeys.all, "listwatched"] as const,
    // Full record for the detail modal, so lists ship lean. `details()` is the
    // prefix enrichment invalidates.
    details: () => [...MoviesKeys.all, "detail"] as const,
    detail: (movieID: number) => [...MoviesKeys.details(), movieID] as const,
    filterOptions: () => [...MoviesKeys.all, "filterOptions"] as const,
}

export const SettingsKeys = {
    all: ["settings"] as const,
    poolLock: () => [...SettingsKeys.all, "poolLock"] as const,
    nextUp: () => [...SettingsKeys.all, "nextUp"] as const,
}

export const StatsKeys = {
    all: ["stats"] as const,
    // Filter ids arrive canonicalized by StatsGetQueryOptions, so selection order
    // cannot split the cache.
    byWindow: (
        window: string,
        timezone: string,
        start: string | undefined,
        end: string | undefined,
        f: { genre?: string; actorIds?: string; crewIds?: string; addedByIds?: string; releaseYear?: number; decade?: number },
    ) =>
      [...StatsKeys.all, "window", window, "tz", timezone, "start", start ?? "", "end", end ?? "", "genre", f.genre ?? "", "actors", f.actorIds ?? "", "crew", f.crewIds ?? "", "addedBy", f.addedByIds ?? "", "releaseYear", f.releaseYear ?? 0, "decade", f.decade ?? 0] as const,
}
