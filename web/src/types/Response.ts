export interface Movie {
    movieID: number;
    title: string;
    link: string;
    addedAt: string;
    addedByID: number;
    addedByName: string;
    watchedAt?: string;
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

export interface TMDBExternalIDs {
    link: string;
}
