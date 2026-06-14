// A credited person on a movie: `character` is set on cast entries, `job` on
// crew entries. profilePath is a raw TMDB path (e.g. "/abc.jpg").
export interface CreditPerson {
    id: number;
    name: string;
    profilePath?: string;
    character?: string;
    job?: string;
}

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

    // TMDB credits (cast in billing order, crew whitelisted jobs only); omitted
    // by the API when empty or not yet enriched.
    cast?: CreditPerson[];
    crew?: CreditPerson[];
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

export interface StatsPersonCount {
    personId: number;
    name: string;
    profilePath?: string;
    count: number;
}

export interface StatsYearCount {
    year: number;
    count: number;
}

export interface StatsRuntime {
    totalMinutes: number;
    averageMinutes: number;
    longestMinutes: number;
    longestTitle?: string;
}

// One person in the stats filters echo, resolved to a display name when any
// credit row references them.
export interface StatsFilterPerson {
    personId: number;
    name?: string;
}

// Echo of the active stats filters, with the people lists resolved to names.
export interface StatsFiltersEcho {
    genre?: string;
    actors?: StatsFilterPerson[];
    crew?: StatsFilterPerson[];
    releaseYear?: number;
    releaseDecade?: number;
}

export interface StatsResponse {
    selectedWindow: StatsWindow;
    selectedWindowCount: number;
    // Movie ids behind selectedWindowCount, watch-recency order — the client
    // joins these to the cached watched list to render the films-in-window rail.
    matchedMovieIDs: number[];
    timezone: string;
    totalWatched: number;
    countsByWindow: StatsWindowCount[];
    watchedByUser: StatsNamedCount[];
    weekdayActivity: StatsNamedCount[];
    hourActivity: StatsHourCount[];
    customRangeStart?: string;
    customRangeEnd?: string;

    // TMDB-metadata aggregates, all computed over the filtered in-window subset.
    topGenres: StatsNamedCount[];
    topDirectors: StatsPersonCount[];
    topActors: StatsPersonCount[];
    releaseYears: StatsYearCount[];
    runtime: StatsRuntime;
    averageRating: number;
    filters: StatsFiltersEcho;
}
