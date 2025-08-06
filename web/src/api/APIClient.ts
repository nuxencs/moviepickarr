import { Movie, Settings, User } from "@/types/Response";

type RequestBody = BodyInit | object | Record<string, unknown> | null;
type Primitive = string | number | boolean | symbol | undefined;

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
        getAll: () => appClient.Get<User[]>("api/users/list"),
        create: (name: string) =>
            appClient.Post<User>("api/users/create", {
                body: { name },
            }),
        delete: (userID: string) =>
            appClient.Delete("api/users/delete", {
                body: { userID },
            }),
        addMovie: (userID: string, title: string, link: string) =>
            appClient.Post<Movie>("api/users/movie/add", {
                body: {
                    userID,
                    title,
                    link,
                },
            }),
        deleteMovie: (userID: string, movieID: string) =>
            appClient.Delete("api/users/movie/delete", {
                body: {
                    userID,
                    movieID,
                },
            }),
        moveMovie: (userID: string, movieID: string) =>
            appClient.Post<Movie>("api/users/movie/move", {
                body: {
                    userID,
                    movieID,
                },
            }),
        getPool: (userID: string) =>
            appClient.Get<Movie[]>("api/users/pool", {
                body: { userID },
            }),
        getStash: (userID: string) =>
            appClient.Get<Movie[]>("api/users/stash", {
                body: { userID },
            }),
    },
    movies: {
        getPooled: () => appClient.Get<Movie[]>("api/movies/listpool"),
        getRandom: () => appClient.Post<Movie>("api/movies/random"),
        getCurrent: () => appClient.Get<Movie>("api/movies/current"),
        getWatched: () =>
            appClient.Get<Movie[]>("api/movies/listwatched"),
        markWatched: () =>
            appClient.Post<Movie[]>("api/movies/markwatched"),
    },
    settings: {
        toggleLock: (lock: boolean) =>
            appClient.Post<Settings>("api/settings/togglelock", {
                body: { lock },
            }),
        getLock: () =>
            appClient.Get<boolean>("api/settings/getlock"),
        getNextPicker: () =>
            appClient.Get<{id: string, name: string}>("api/settings/nextpicker"),
    }
};
