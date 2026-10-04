import { getClientId } from "@/lib/clientId";
import { AuthConfig, ClaimInfo, FilterOptionsResponse, InviteResult, InvitesResponse, MeResponse, MemberRole, MovieDetail, MovieDrawPayload, MovieTile, MoveTarget, RemoveResult, RosterMember, SessionSummary, Settings, StatsResponse, StatsWindow, TMDBMovie, User, Wildcard } from "@/types/Response";

// Carries the HTTP status so callers can branch on it (login shows its banner only on a 401).
export class ApiError extends Error {
    readonly status: number;
    readonly code?: string;

    constructor(status: number, message: string, code?: string) {
        super(message);
        this.name = "ApiError";
        this.status = status;
        this.code = code;
    }
}

type RequestBody = BodyInit | object | Record<string, unknown> | null;
type Primitive = string | number | boolean | symbol | undefined;

interface StatsQuery {
    window: StatsWindow;
    timezone: string;
    start?: string;
    end?: string;
    genre?: string;
    // Comma-joined id lists: the backend reads ONE param, and an array would repeat
    // it and lose all but the first.
    actorIds?: string;
    crewIds?: string;
    // Comma-joined, like actorIds.
    addedByIds?: string;
    releaseYear?: number;
    // Decade floor (1990 ⇒ 1990–1999); mutually exclusive with releaseYear.
    decade?: number;
}

interface HttpConfig {
    method?: string;
    body?: RequestBody;
    queryString?: Record<string, Primitive | Primitive[]>;
    // Lets superseded requests (rapid /stats filter changes) cancel in flight.
    signal?: AbortSignal;
}

function encodeRFC3986URIComponent(str: string): string {
    return encodeURIComponent(str).replace(
        /[!'()*]/g,
        (c) => `%${c.charCodeAt(0).toString(16).toUpperCase()}`,
    );
}

function baseURL(): string {
    // Dev: same-origin via the Vite proxy (see vite.config.ts), so no preflight.
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
    // RFC 7807 errors are application/problem+json, so match the +json suffix too.
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
        // Messages match what components already toast.
        switch (response.status) {
            case 400:
                return Promise.reject(new ApiError(400, "Bad request"));
            case 404:
                return Promise.reject(new ApiError(404, "Not Found"));
            case 500:
                return Promise.reject(new ApiError(500, "Internal Server Error"));
            default:
                break;
        }

        let reason = "";
        let code: string | undefined;
        if (isJSON) {
            const json = await response.json();
            // problem+json puts the text in "detail" (else "title"); older shapes use "message".
            if (typeof json.detail === "string" && json.detail.length) {
                reason = json.detail as string;
            } else if (typeof json.message === "string" && json.message.length) {
                reason = json.message as string;
            } else if (typeof json.title === "string" && json.title.length) {
                reason = json.title as string;
            }
            if (typeof json.title === "string" && json.title.length) {
                code = json.title as string;
            }
        }

        // Server reasons are written for humans; components toast err.message verbatim.
        if (reason.length) {
            return Promise.reject(new ApiError(response.status, reason, code));
        }

        const statusText = response.statusText.length
            ? ` (${response.statusText})`
            : "";
        return Promise.reject(
            new ApiError(
                response.status,
                `HTTP request to ${endpoint} failed with code ${response.status}${statusText}`,
            ),
        );
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
    Patch: <T = void>(endpoint: string, config: HttpConfig = {}) =>
        HttpClient<T>(endpoint, {
            ...config,
            method: "PATCH",
        }),
    Delete: <T = void>(endpoint: string, config: HttpConfig = {}) =>
        HttpClient<T>(endpoint, {
            ...config,
            method: "DELETE",
        }),
};

// Top-level navigations, not XHR: the server 302s to the provider and cookies ride along.
export const oidcLoginPath = () => "/api/v1/auth/oidc/login";
export const oidcClaimPath = (token: string) =>
    `/api/v1/auth/claim/${encodeRFC3986URIComponent(token)}/oidc`;
// Same navigation; the callback returns to /settings?linked=1 or ?error=<bucket>.
export const oidcLinkPath = () => "/api/v1/auth/oidc/link";

export const APIClient = {
    auth: {
        // Public: what the unauthenticated login page needs (SSO presence).
        config: () => appClient.Get<AuthConfig>("api/v1/auth/config"),
        // Public. [] when the cache is unwarmed or no TMDB key is set.
        posterWall: () => appClient.Get<string[]>("api/v1/auth/poster-wall"),
        // The session actor; rejects 401 when there is no valid session.
        me: () => appClient.Get<MeResponse>("api/v1/auth/me"),
        // 204 + session cookie on success; 401 for any credential failure.
        login: (username: string, password: string) =>
            appClient.Post<void>("api/v1/auth/login", { body: { username, password } }),
        // 404 = no longer valid, 410 = already set up.
        validateClaim: (token: string) =>
            appClient.Get<ClaimInfo>(`api/v1/auth/claim/${encodeRFC3986URIComponent(token)}`),
        // Reset mode omits username. 204 + session cookie on success.
        claimPassword: (token: string, password: string, username?: string) =>
            appClient.Post<void>(`api/v1/auth/claim/${encodeRFC3986URIComponent(token)}/password`, {
                body: username ? { username, password } : { password },
            }),
        // Revokes other devices and rotates this session's cookie, so this device stays
        // signed in. 401 on a wrong current password.
        changePassword: (currentPassword: string, newPassword: string) =>
            appClient.Post<void>("api/v1/auth/password", { body: { currentPassword, newPassword } }),
        // An SSO-first member (no local login) adds a first username + password.
        setPassword: (username: string, password: string) =>
            appClient.Post<void>("api/v1/auth/local-login", { body: { username, password } }),
        // { all: true } ends every session, this one included.
        logout: (all = false) =>
            appClient.Post<void>("api/v1/auth/logout", { body: all ? { all: true } : {} }),
        // Self-only: the member comes from the session.
        sessions: () => appClient.Get<SessionSummary[]>("api/v1/auth/sessions"),
        // 404 when the session is gone or is not yours.
        revokeSession: (sessionID: string) =>
            appClient.Delete(`api/v1/auth/sessions/${encodeRFC3986URIComponent(sessionID)}`),
    },
    // Admin actions act as the session actor. 403 is the "Admins only" signal.
    members: {
        roster: () => appClient.Get<RosterMember[]>("api/v1/members/roster"),
        // The claim URL is response-only (never broadcast) and shown once.
        create: (name: string, role: MemberRole) =>
            appClient.Post<InviteResult>("api/v1/members", { body: { name, role } }),
        // A Next up holder needs a confirmed retry before a Guest change hands off the turn.
        setRole: (memberID: number, role: MemberRole, confirmTurnHandoff = false) =>
            appClient.Patch<void>(`api/v1/members/${memberID}/role`, {
                body: { role, confirmTurnHandoff },
            }),
        // First generation only; existing ones are replaced through invites.replace.
        createInvite: (memberID: number) =>
            appClient.Post<InviteResult>(`api/v1/members/${memberID}/invite`),
        createPasswordResetInvite: (memberID: number) =>
            appClient.Post<InviteResult>(`api/v1/members/${memberID}/invite`, {
                body: { purpose: "password_reset" },
            }),
        // Reset revokes the member's other sessions.
        setLocalLogin: (memberID: number, username: string, password: string) =>
            appClient.Put<void>(`api/v1/members/${memberID}/local-login`, { body: { username, password } }),
        removeLocalLogin: (memberID: number) =>
            appClient.Delete(`api/v1/members/${memberID}/local-login`),
        // Remove another member's linked identity (they fall back to a placeholder).
        unlink: (memberID: number) =>
            appClient.Delete(`api/v1/members/${memberID}/linked-identity`),
        // 409 on your last credential (server backstop for the client check).
        unlinkSelf: () => appClient.Delete("api/v1/auth/linked-identity"),
        // One action, two outcomes: hard delete (no authored movies) or archive.
        remove: (memberID: number) =>
            appClient.Delete<RemoveResult>(`api/v1/members/${memberID}`),
        // Reactivate an archived member and re-issue their claim link in one step.
        restore: (memberID: number) =>
            appClient.Post<InviteResult>(`api/v1/members/${memberID}/restore`),
    },
    // Existing invite generations are addressed only by immutable public id,
    // so a stale tab cannot mutate a replacement generation for the member.
    invites: {
        list: () => appClient.Get<InvitesResponse>("api/v1/invites"),
        replace: (inviteID: string) =>
            appClient.Post<InviteResult>(
                `api/v1/invites/${encodeRFC3986URIComponent(inviteID)}/replacement`,
            ),
        revoke: (inviteID: string) =>
            appClient.Delete(`api/v1/invites/${encodeRFC3986URIComponent(inviteID)}`),
        dismiss: (inviteID: string) =>
            appClient.Post(`api/v1/invites/${encodeRFC3986URIComponent(inviteID)}/dismiss`),
    },
    // Movie mutations are adder-only server-side (403 not_adder, no admin override),
    // so none take a member id. Member lifecycle lives under `members`.
    board: {
        getAll: () => appClient.Get<User[]>("api/v1/members"),
        // Adds always land in the session member's stash.
        addMovie: (title: string, tmdbId: number) =>
            appClient.Post<MovieDetail>("api/v1/movies", {
                body: {
                    title,
                    tmdbId,
                },
            }),
        deleteMovie: (movieID: number) =>
            appClient.Delete(`api/v1/movies/${movieID}`),
        updateMovie: (movieID: number, title: string, link: string, watchedAt?: string) =>
            appClient.Put<MovieDetail>(`api/v1/movies/${movieID}`, {
                body: {
                    title,
                    link,
                    watchedAt,
                },
            }),
        moveMovie: (movieID: number, target: MoveTarget) =>
            appClient.Post<void>(`api/v1/movies/${movieID}/move`, {
                body: { target },
            }),
        getPool: (userID: number) =>
            appClient.Get<MovieTile[]>(`api/v1/members/${userID}/pool`),
        getStash: (userID: number) =>
            appClient.Get<MovieTile[]>(`api/v1/members/${userID}/stash`),
    },
    movies: {
        getPooled: () => appClient.Get<MovieTile[]>("api/v1/movies/pool"),
        // Identify the drawer so only this client shows the reel's confirm button.
        getRandom: () =>
            appClient.Post<MovieDrawPayload>("api/v1/movies/random", {
                body: { clientId: getClientId() },
            }),
        getCurrent: () => appClient.Get<MovieDetail | null>("api/v1/movies/current"),
        getWildcard: () => appClient.Get<Wildcard | null>("api/v1/movies/wildcard"),
        selectWildcard: (hostMovieID: number, movieID: number) =>
            appClient.Post<Wildcard>("api/v1/movies/wildcard", {
                body: { hostMovieId: hostMovieID, movieId: movieID },
            }),
        selectTMDBWildcard: (hostMovieID: number, title: string, tmdbID: number) =>
            appClient.Post<Wildcard>("api/v1/movies/wildcard", {
                body: { hostMovieId: hostMovieID, title, tmdbId: tmdbID },
            }),
        cancelWildcard: (wildcardID: number) => appClient.Delete<{ id: number; movieId: number }>(`api/v1/movies/wildcard?wildcardId=${wildcardID}`),
        watchWildcard: (wildcardID: number) => appClient.Post<Wildcard>("api/v1/movies/wildcard/watch", {
            body: { wildcardId: wildcardID },
        }),
        // Closes the reel for every client (via movie:revealed).
        reveal: () => appClient.Post<void>("api/v1/movies/current/reveal"),
        getWatched: () =>
            appClient.Get<MovieTile[]>("api/v1/movies/watched"),
        // Full record for the detail modal; list payloads are lean.
        get: (movieID: number, signal?: AbortSignal) =>
            appClient.Get<MovieDetail>(`api/v1/movies/${movieID}`, { signal }),
        getFilterOptions: (signal?: AbortSignal) =>
            appClient.Get<FilterOptionsResponse>("api/v1/movies/filter-options", { signal }),
        markWatched: () =>
            appClient.Post<MovieDetail>("api/v1/movies/current/watch"),
    },
    settings: {
        toggleLock: (lock: boolean) =>
            appClient.Put<Settings>("api/v1/settings/pool-lock", {
                body: { poolLocked: lock },
            }),
        // One pool-state read for both mutation gates. A draw can hold the pool
        // even when this client skips the reel (reduced motion, one candidate).
        getPoolState: () =>
            appClient.Get<Settings>("api/v1/settings/pool-lock"),
        getNextUp: () =>
            appClient.Get<{id: number, name: string}>("api/v1/settings/next-up"),
        // Names the holder the admin saw; a moved turn answers 409 next_up_changed.
        skipNextUp: (memberId: number) =>
            appClient.Post<{id: number, name: string}>("api/v1/settings/next-up/skip", {
                body: { memberId },
            }),
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
