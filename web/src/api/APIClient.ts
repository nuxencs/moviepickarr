import { getClientId } from "@/lib/clientId";
import { FilterOptionsResponse, Movie, Settings, StatsResponse, StatsWindow, TMDBMovie, User } from "@/types/Response";

type RequestBody = BodyInit | object | Record<string, unknown> | null;
type Primitive = string | number | boolean | symbol | undefined;

interface StatsQuery {
    window: StatsWindow;
    timezone: string;
    start?: string;
    end?: string;
    genre?: string;
    // Comma-joined TMDB person id lists. The backend reads each as ONE query
    // param, so they're pre-joined strings — an array here would serialize to
    // repeated params and all but the first would be dropped server-side.
    actorIds?: string;
    crewIds?: string;
    // Comma-joined user ids of the movie adders (pre-joined, like actorIds).
    addedByIds?: string;
    releaseYear?: number;
    // Decade floor (1990 ⇒ 1990–1999); mutually exclusive with releaseYear.
    decade?: number;
}

interface HttpConfig {
    method?: string;
    body?: RequestBody;
    queryString?: Record<string, Primitive | Primitive[]>;
    // React Query's per-call AbortSignal; threading it lets superseded requests
    // (e.g. rapid /stats filter changes under keepPreviousData) cancel in flight.
    signal?: AbortSignal;
}

function encodeRFC3986URIComponent(str: string): string {
    return encodeURIComponent(str).replace(
        /[!'()*]/g,
        (c) => `%${c.charCodeAt(0).toString(16).toUpperCase()}`,
    );
}

function baseURL(): string {
    // In dev, return "" so requests hit "/api/..." same-origin and ride the
    // Vite proxy (see vite.config.ts). Same-origin requests never preflight.
    if (import.meta.env.DEV) {
        return "";
    }

    return window.location.origin;
}

export async function HttpClient<T = unknown>(
    endpoint: string,
    config: HttpConfig = {},
): Promise<T> {
    const init: RequestInit = {
        method: config.method,
        headers: { Accept: "*/*" },
        credentials: "include",
        signal: config.signal,
    };

    if (config.body) {
        init.body = JSON.stringify(config.body);

        if (typeof config.body === "object") {
            init.headers = {
                ...init.headers,
                "Content-Type": "application/json",
            };
        }
    }

    if (config.queryString) {
        const params: string[] = [];

        for (const [key, value] of Object.entries(config.queryString)) {
            const serializedKey = encodeRFC3986URIComponent(key);

            if (typeof value === "undefined") {
                continue;
            } else if (Array.isArray(value)) {
                value.forEach((child) => {
                    const v = typeof child !== "undefined" ? String(child) : "";
                    if (v.length) {
                        params.push(`${serializedKey}=${encodeRFC3986URIComponent(v)}`);
                    }
                });
            } else {
                const v = String(value);
                if (v.length) {
                    params.push(`${serializedKey}=${encodeRFC3986URIComponent(v)}`);
                }
            }
        }

        if (params.length) {
            endpoint += `?${params.join("&")}`;
        }
    }

    const response = await window.fetch(`${baseURL()}/${endpoint}`, init);
    const contentType = response.headers.get("Content-Type") ?? "";
    // Errors arrive as RFC 7807 "application/problem+json", so match the +json
    // suffix too — a plain "application/json" check misses them.
    const isJSON =
        contentType.includes("application/json") || contentType.includes("+json");

    if (response.status >= 200 && response.status < 300) {
        if (response.status === 204) {
            return Promise.resolve<T>({} as T);
        }

        if (isJSON) {
            return Promise.resolve<T>((await response.json()) as T);
        } else {
            return Promise.resolve<T>(response as T);
        }
    } else {
        switch (response.status) {
            case 400:
                return Promise.reject(new Error("Bad request"));
            case 404:
                return Promise.reject(new Error("Not Found"));
            case 500:
                return Promise.reject(new Error("Internal Server Error"));
            default:
                break;
        }

        let reason = "";
        if (isJSON) {
            const json = await response.json();
            // problem+json carries the human-readable text in "detail"
            // (fallback "title"); older shapes used "message".
            if (typeof json.detail === "string" && json.detail.length) {
                reason = json.detail as string;
            } else if (typeof json.message === "string" && json.message.length) {
                reason = json.message as string;
            } else if (typeof json.title === "string" && json.title.length) {
                reason = json.title as string;
            }
        }

        // A server-provided reason is written for humans (e.g. "conflict:
        // movie is already in the library") — surface it as the message
        // directly; components toast err.message verbatim.
        if (reason.length) {
            return Promise.reject(new Error(reason));
        }

        const statusText = response.statusText.length
            ? ` (${response.statusText})`
            : "";
        const defaultError = new Error(
            `HTTP request to ${endpoint} failed with code ${response.status}${statusText}`,
        );
        return Promise.reject(defaultError);
    }
}

const appClient = {
    Get: <T>(endpoint: string, config: HttpConfig = {}) =>
        HttpClient<T>(endpoint, {
            ...config,
            method: "GET",
        }),
    Put: <T = void>(endpoint: string, config: HttpConfig = {}) =>
        HttpClient<T>(endpoint, {
            ...config,
            method: "PUT",
        }),
    Post: <T = void>(endpoint: string, config: HttpConfig = {}) =>
        HttpClient<T>(endpoint, {
            ...config,
            method: "POST",
        }),
    Delete: (endpoint: string, config: HttpConfig = {}) =>
        HttpClient<void>(endpoint, {
            ...config,
            method: "DELETE",
        }),
};

export const APIClient = {
    users: {
        getAll: () => appClient.Get<User[]>("api/v1/users"),
        create: (name: string) =>
            appClient.Post<User>("api/v1/users", {
                body: { name },
            }),
        delete: (userID: number) =>
            appClient.Delete(`api/v1/users/${userID}`),
        addMovie: (userID: number, title: string, tmdbId: number) =>
            appClient.Post<Movie>(`api/v1/users/${userID}/movies`, {
                body: {
                    title,
                    tmdbId,
                },
            }),
        deleteMovie: (userID: number, movieID: number) =>
            appClient.Delete(`api/v1/users/${userID}/movies/${movieID}`),
        updateMovie: (userID: number, movieID: number, title: string, link: string, watchedAt?: string) =>
            appClient.Put<Movie>(`api/v1/users/${userID}/movies/${movieID}`, {
                body: {
                    title,
                    link,
                    watchedAt,
                },
            }),
        moveMovie: (userID: number, movieID: number, target: "pool" | "stash") =>
            appClient.Post<Movie>(`api/v1/users/${userID}/movies/${movieID}/move`, {
                body: { target },
            }),
        getPool: (userID: number) =>
            appClient.Get<Movie[]>(`api/v1/users/${userID}/pool`),
        getStash: (userID: number) =>
            appClient.Get<Movie[]>(`api/v1/users/${userID}/stash`),
    },
    movies: {
        getPooled: () => appClient.Get<Movie[]>("api/v1/movies/pool"),
        // Identify the picker so only this client shows the reel's confirm button.
        getRandom: () =>
            appClient.Post<Movie>("api/v1/movies/random", {
                body: { clientId: getClientId() },
            }),
        getCurrent: () => appClient.Get<Movie>("api/v1/movies/current"),
        // Confirm the pick — closes the reel for every client (via movie:revealed).
        reveal: () => appClient.Post<void>("api/v1/movies/current/reveal"),
        getWatched: () =>
            appClient.Get<Movie[]>("api/v1/movies/watched"),
        // Full enriched record (cast/crew/overview/backdrop) for the detail modal;
        // the list payloads are lean, so the modal lazy-loads this on open.
        get: (movieID: number, signal?: AbortSignal) =>
            appClient.Get<Movie>(`api/v1/movies/${movieID}`, { signal }),
        // Stats filter choices, derived server-side from the watched library.
        getFilterOptions: (signal?: AbortSignal) =>
            appClient.Get<FilterOptionsResponse>("api/v1/movies/filter-options", { signal }),
        markWatched: () =>
            appClient.Post<Movie[]>("api/v1/movies/current/watch"),
    },
    settings: {
        toggleLock: (lock: boolean) =>
            appClient.Put<Settings>("api/v1/settings/pool-lock", {
                body: { poolLocked: lock },
            }),
        getLock: () =>
            appClient.Get<boolean>("api/v1/settings/pool-lock"),
        getNextPicker: () =>
            appClient.Get<{id: number, name: string}>("api/v1/settings/next-picker"),
    },
    stats: {
        get: ({ window, timezone, start, end, genre, actorIds, crewIds, addedByIds, releaseYear, decade }: StatsQuery, signal?: AbortSignal) =>
            appClient.Get<StatsResponse>("api/v1/stats", {
                queryString: {
                    window,
                    tz: timezone,
                    start,
                    end,
                    genre,
                    actorIds,
                    crewIds,
                    addedByIds,
                    releaseYear,
                    decade,
                },
                signal,
            }),
    },
    tmdb: {
        search: (query: string, signal?: AbortSignal) =>
            appClient.Get<TMDBMovie[]>(
                "api/v1/tmdb/search",
                { queryString: { query }, signal }
            ),
    }
};
