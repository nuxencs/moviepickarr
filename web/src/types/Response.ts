export interface Movie {
    movieID: string;
    title: string;
    link: string;
    addedAt: string;
    addedByID: string;
    addedByName: string;
    watchedAt?: string;
}

export interface User {
    userID: string;
    name: string;
    currentPool: Record<string, Movie>;
    stash: Record<string, Movie>;
    createdAt: string;
}

export interface Settings {
    poolLocked: boolean;
}
