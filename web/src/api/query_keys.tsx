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

export const SettingsKeys = {
    all: ["settings"] as const,
    poolLock: () => [...SettingsKeys.all, "poolLock"] as const,
    nextPicker: () => [...SettingsKeys.all, "nextPicker"] as const,
}

export const StatsKeys = {
    all: ["stats"] as const,
    byWindow: (window: string, timezone: string, start?: string, end?: string) =>
      [...StatsKeys.all, "window", window, "tz", timezone, "start", start ?? "", "end", end ?? ""] as const,
}
