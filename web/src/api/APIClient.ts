import { getClientId } from "@/lib/clientId";
import { AuthConfig, ClaimInfo, FilterOptionsResponse, InviteResult, InvitesResponse, MeResponse, MovieDetail, MovieDrawPayload, MovieTile, MoveTarget, RemoveResult, RosterMember, SessionSummary, Settings, StatsResponse, StatsWindow, TMDBMovie, User } from "@/types/Response";

// Carries the HTTP status alongside the human-readable message so callers can
// branch on it (the login page shows the uniform banner only for a 401, and
// treats anything else as a try-again error). It extends Error, so existing
// `err.message` consumers are unaffected.
export class ApiError extends Error {
    readonly status: number;

    constructor(status: number, message: string) {
        super(message);
        this.name = "ApiError";
        this.status = status;
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
        // Every rejection is an ApiError carrying the status, so callers can
        // branch on it; the message text matches what components already toast.
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
            return Promise.reject(new ApiError(response.status, reason));
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

// OIDC initiation and claim-via-SSO are top-level browser navigations (the
// server 302s to the provider), not XHR. These same-origin paths drive a
// `window.location.assign`, so the session/tx cookies ride along.
export const oidcLoginPath = () => "/api/v1/auth/oidc/login";
export const oidcClaimPath = (token: string) =>
    `/api/v1/auth/claim/${encodeRFC3986URIComponent(token)}/oidc`;
// Linking SSO to the signed-in member is the same top-level navigation: the
// server 302s to the provider and, on the callback, back to /settings?linked=1
// (or ?error=<bucket>). Driven by window.location.assign so the session cookie
// rides along.
export const oidcLinkPath = () => "/api/v1/auth/oidc/link";

export const APIClient = {
    auth: {
        // Public: what the unauthenticated login page needs (SSO presence).
        config: () => appClient.Get<AuthConfig>("api/v1/auth/config"),
        // Public: popularity-ordered TMDB poster paths for the login wall. Bare
        // []string; [] when the cache is unwarmed or no TMDB key is set. Carries
        // no secrets (poster paths are public artwork).
        posterWall: () => appClient.Get<string[]>("api/v1/auth/poster-wall"),
        // The session actor; rejects 401 when there is no valid session.
        me: () => appClient.Get<MeResponse>("api/v1/auth/me"),
        // 204 + session cookie on success; 401 for any credential failure.
        login: (username: string, password: string) =>
            appClient.Post<void>("api/v1/auth/login", { body: { username, password } }),
        // Claim-page data for a token: the greet name, placeholder-vs-reset mode,
        // and offered options. 404 = no longer valid, 410 = already set up.
        validateClaim: (token: string) =>
            appClient.Get<ClaimInfo>(`api/v1/auth/claim/${encodeRFC3986URIComponent(token)}`),
        // Redeem via password. Placeholder sends username + password; reset sends
        // password only (username omitted). 204 + session cookie on success.
        claimPassword: (token: string, password: string, username?: string) =>
            appClient.Post<void>(`api/v1/auth/claim/${encodeRFC3986URIComponent(token)}/password`, {
                body: username ? { username, password } : { password },
            }),
        // Self-service account actions (the session is the proof of identity).
        // Change an existing password: verify the current one, rewrite it. The
        // server revokes the other devices and rotates this session's cookie, so
        // the round trip keeps this device signed in. 401 on a wrong current.
        changePassword: (currentPassword: string, newPassword: string) =>
            appClient.Post<void>("api/v1/auth/password", { body: { currentPassword, newPassword } }),
        // An SSO-first member (no local login) adds a first username + password.
        setPassword: (username: string, password: string) =>
            appClient.Post<void>("api/v1/auth/local-login", { body: { username, password } }),
        // Log out. Empty body ends this device; { all: true } ends every session
        // for the member (this one included). 204 + cleared cookie either way.
        logout: (all = false) =>
            appClient.Post<void>("api/v1/auth/logout", { body: all ? { all: true } : {} }),
        // The actor's own live sessions, most recently active first. Self-only
        // server-side: the member comes from the session, so there is no id to
        // pass and no way to read anyone else's devices.
        sessions: () => appClient.Get<SessionSummary[]>("api/v1/auth/sessions"),
        // Sign one of your own devices out. 204; 404 when the session is already
        // gone or was never yours (the delete is scoped to the session member,
        // so another member's public handle matches nothing).
        revokeSession: (sessionID: string) =>
            appClient.Delete(`api/v1/auth/sessions/${encodeRFC3986URIComponent(sessionID)}`),
    },
    // The admin roster surface. Reads the presence-derived roster and drives every
    // per-member admin action off the session actor (never a path id for the actor).
    // 403 on any of these is the "Admins only" signal the surface renders.
    members: {
        roster: () => appClient.Get<RosterMember[]>("api/v1/members/roster"),
        // Create a placeholder + issue its first claim link in one step; the claim
        // URL is response-only (never broadcast) and shown once.
        create: (name: string) =>
            appClient.Post<InviteResult>("api/v1/members", { body: { name } }),
        // Promote/demote. 409 when it would demote the last admin.
        setRole: (memberID: number, role: "member" | "admin") =>
            appClient.Patch<void>(`api/v1/members/${memberID}/role`, { body: { role } }),
        // Create the first current invite generation. Existing generations are
        // replaced through their immutable handle below.
        createInvite: (memberID: number) =>
            appClient.Post<InviteResult>(`api/v1/members/${memberID}/invite`),
        // Issue a recovery link for a member who already has a local login.
        createPasswordResetInvite: (memberID: number) =>
            appClient.Post<InviteResult>(`api/v1/members/${memberID}/invite`, {
                body: { purpose: "password_reset" },
            }),
        // Set (create) or reset an existing local login. Reset revokes the member's
        // other sessions server-side.
        setLocalLogin: (memberID: number, username: string, password: string) =>
            appClient.Put<void>(`api/v1/members/${memberID}/local-login`, { body: { username, password } }),
        removeLocalLogin: (memberID: number) =>
            appClient.Delete(`api/v1/members/${memberID}/local-login`),
        // Remove another member's linked identity (they fall back to a placeholder).
        unlink: (memberID: number) =>
            appClient.Delete(`api/v1/members/${memberID}/linked-identity`),
        // Remove your OWN linked identity. 409 when it is your last credential (the
        // surface refuses this client-side first; this is the backstop).
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
    // The Members board and its self-service movie actions. Reads hit /members
    // (the board's per-member pool + stash tiles); mutations hit /movies. Movie
    // mutations are adder-only server-side: the adder is always the session
    // member, so none of them take a target member id (editing/moving/deleting a
    // movie you did not add returns 403 not_adder, with no admin override).
    // Member lifecycle (create/remove) lives under `members` above, not here.
    board: {
        // Every member with their lean pool + stash tiles for the board.
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
        // Board reads stay keyed by member id (a public per-member read, not a
        // mutation): the pool/stash tiles for the given member.
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
        // Confirm the draw — closes the reel for every client (via movie:revealed).
        reveal: () => appClient.Post<void>("api/v1/movies/current/reveal"),
        getWatched: () =>
            appClient.Get<MovieTile[]>("api/v1/movies/watched"),
        // Full enriched record (cast/crew/overview/backdrop) for the detail modal;
        // the list payloads are lean, so the modal lazy-loads this on open.
        get: (movieID: number, signal?: AbortSignal) =>
            appClient.Get<MovieDetail>(`api/v1/movies/${movieID}`, { signal }),
        // Stats filter choices, derived server-side from the watched library.
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
