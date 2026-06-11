export interface Movie {
    movieID: number;
    title: string;
    link: string;
    addedAt: string;
    addedByID: number;
    addedByName: string;
    watchedAt?: string;

    // Stable external identities — used to build IMDb / TMDB / Letterboxd links.
    tmdbId?: number;
    imdbId?: string;

    // Enriched TMDB metadata — all optional (a movie may not be enriched yet).
    // posterPath/backdropPath are raw TMDB paths (e.g. "/abc.jpg").
    posterPath?: string;
    backdropPath?: string;
    releaseDate?: string;
    runtime?: number;
    genres?: string[];
    voteAverage?: number;
    tagline?: string;
    overview?: string;
}

export interface User {
    userID: number;
    name: string;
    currentPool: Record<string, Movie>;
    stash: Record<string, Movie>;
    createdAt: string;
}

export interface Settings {
    poolLocked: boolean;
}

export interface TMDBMovie {
    id: number;
    title: string;
    poster_path: string | null;
    release_date: string;
    overview: string;
}

export type StatsWindow = "24h" | "7d" | "30d" | "90d" | "1y" | "all-time" | "custom";

export interface StatsWindowCount {
    window: StatsWindow;
    count: number;
}

export interface StatsNamedCount {
    name: string;
    count: number;
}

export interface StatsHourCount {
    hour: number;
    label: string;
    count: number;
}

export interface StatsResponse {
    selectedWindow: StatsWindow;
    selectedWindowCount: number;
    timezone: string;
    totalWatched: number;
    countsByWindow: StatsWindowCount[];
    watchedByUser: StatsNamedCount[];
    weekdayActivity: StatsNamedCount[];
    hourActivity: StatsHourCount[];
    customRangeStart?: string;
    customRangeEnd?: string;
}
