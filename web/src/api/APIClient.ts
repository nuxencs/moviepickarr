import { Movie, Settings, StatsResponse, StatsWindow, TMDBExternalIDs, TMDBMovie, User } from "@/types/Response";

type RequestBody = BodyInit | object | Record<string, unknown> | null;
type Primitive = string | number | boolean | symbol | undefined;

interface StatsQuery {
    window: StatsWindow;
    timezone: string;
    start?: string;
    end?: string;
}

interface HttpConfig {
    method?: string;
    body?: RequestBody;
    queryString?: Record<string, Primitive | Primitive[]>;
}

function encodeRFC3986URIComponent(str: string): string {
    return encodeURIComponent(str).replace(
        /[!'()*]/g,
        (c) => `%${c.charCodeAt(0).toString(16).toUpperCase()}`,
    );
}

function baseURL(): string {
    if (import.meta.env.DEV) {
        return "http://localhost:3030";
    }

    return window.location.origin;
}

export async function HttpClient<T = unknown>(
    endpoint: string,
    config: HttpConfig = {},
): Promise<T> {
    const init: RequestInit = {
        method: config.method,
        headers: { Accept: "*/*", "x-requested-with": "XMLHttpRequest" },
        credentials: "include",
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
    const isJSON = response.headers
        .get("Content-Type")
        ?.includes("application/json");

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

        let reason = response.statusText;
        if (isJSON) {
            const json = await response.json();
            if ("message" in json) {
                reason = json.message as string;
            }
        }

        if (reason.length) {
            reason = ` (${reason})`;
        }

        const defaultError = new Error(
            `HTTP request to ${endpoint} failed with code ${response.status}${reason}`,
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
        addMovie: (userID: number, title: string, link: string) =>
            appClient.Post<Movie>(`api/v1/users/${userID}/movies`, {
                body: {
                    title,
                    link,
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
        moveMovie: (userID: number, movieID: number) =>
            appClient.Post<Movie>(`api/v1/users/${userID}/movies/${movieID}/move`),
        getPool: (userID: number) =>
            appClient.Get<Movie[]>(`api/v1/users/${userID}/pool`),
        getStash: (userID: number) =>
            appClient.Get<Movie[]>(`api/v1/users/${userID}/stash`),
    },
    movies: {
        getPooled: () => appClient.Get<Movie[]>("api/v1/movies/pool"),
        getRandom: () => appClient.Post<Movie>("api/v1/movies/random"),
        getCurrent: () => appClient.Get<Movie>("api/v1/movies/current"),
        getWatched: () =>
            appClient.Get<Movie[]>("api/v1/movies/watched"),
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
        get: ({ window, timezone, start, end }: StatsQuery) =>
            appClient.Get<StatsResponse>("api/v1/stats", {
                queryString: {
                    window,
                    tz: timezone,
                    start,
                    end,
                },
            }),
    },
    tmdb: {
        search: (query: string) =>
            appClient.Get<TMDBMovie[]>(
                "api/v1/tmdb/search",
                { queryString: { query } }
            ),
        getExternalIds: (movieId: number) =>
            appClient.Get<TMDBExternalIDs>(
                "api/v1/tmdb/external-ids",
                { queryString: { movieId: String(movieId) } }
            ),
    }
};
