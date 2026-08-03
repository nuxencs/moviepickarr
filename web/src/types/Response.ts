// A credited person on a movie: `character` is set on cast entries, `job` on
// crew entries. profilePath is a raw TMDB path (e.g. "/abc.jpg").
export interface CreditPerson {
    id: number;
    name: string;
    profilePath?: string;
    character?: string;
    job?: string;
}

// Where a film sits in the app. Mirrors the server's domain.MovieStatus.
export type MovieStatus = "pool" | "stash" | "current" | "watched";

// The two statuses a member can move a film between (POST /movies/:id/move).
// Derived from MovieStatus so renaming a status breaks here too.
export type MoveTarget = Extract<MovieStatus, "pool" | "stash">;

// The list/tile wire class. Pool, watched, and member-board reads return only
// these fields; modal-only metadata is structurally unavailable until detail
// is fetched.
export interface MovieTile {
    movieID: number;
    title: string;
    link: string;
    addedAt: string;
    addedByID: number;
    addedByName: string;
    // Archived adders keep attribution but no longer have a Members board.
    // Omitted for active adders to keep list payloads unchanged.
    addedByArchived?: boolean;
    watchedAt?: string;

    // Stable external identities used to build IMDb / TMDB / Letterboxd links.
    tmdbId?: number;
    imdbId?: string;

    // Enriched tile metadata. All optional while enrichment is pending.
    posterPath?: string;
    releaseDate?: string;
    runtime?: number;
    genres?: string[];
    voteAverage?: number;
}

// The full/detail wire class. Detail, current, mutation, and movie lifecycle
// payloads carry it. A held winner remains projected as pooled until reveal.
export interface MovieDetail extends MovieTile {
    status: MovieStatus;

    // Draw-reveal coordination — present only on the current-movie endpoint and
    // the movie:drawn event. drawnAt is when the current movie was drawn;
    // revealAt is the server's auto-reveal deadline (the confirm countdown is
    // derived from it, the server owns the reveal timing); serverNow is the
    // server clock at fetch time, so the client computes the reveal spin's
    // elapsed time without trusting its own clock.
    drawnAt?: string;
    revealAt?: string;
    serverNow?: string;
    // drawClientId is the client that initiated the draw — only that client
    // shows the reel's confirm (OK) button. revealed reports whether the draw has
    // been confirmed, so a reload after the reveal skips the reel (see drawSpin).
    drawClientId?: string;
    revealed?: boolean;
    // Modal-only enriched metadata. All optional while enrichment is pending;
    // backdropPath is a raw TMDB path (e.g. "/abc.jpg").
    backdropPath?: string;
    tagline?: string;
    overview?: string;

    // TMDB credits (cast in billing order, crew whitelisted jobs only); omitted
    // by the API when empty or not yet enriched.
    cast?: CreditPerson[];
    crew?: CreditPerson[];
}

// The draw mutation and movie:drawn event add a self-contained, lean reel
// source to the full winning record. The exceptional recovery broadcast may
// omit candidates, in which case clients skip the reel.
export interface MovieDrawPayload extends MovieDetail {
    candidates?: MovieTile[];
}

export interface User {
    userID: number;
    name: string;
    currentPool: Record<string, MovieTile>;
    stash: Record<string, MovieTile>;
    createdAt: string;
}

export interface Settings {
    poolLocked: boolean;
    // Server-owned pool freeze during an unrevealed draw. Independent of
    // whether this client is animating a reel.
    drawInProgress: boolean;
}

// One row of the admin roster (GET /members/roster). Login state is
// presence-derived server-side, never a stored flag: hasLocalLogin /
// hasLinkedIdentity / invitePending are the existence of a credential / invite
// row, archived the archived_at column. moviesAuthored decides whether a remove
// hard-deletes (frees the name) or archives (keeps attribution), so the surface
// can name the outcome before committing. username is present only with a local
// login; lastSeenAt is the newest session touch, absent for members who never
// logged in.
export interface RosterMember {
    id: number;
    name: string;
    username?: string;
    role: "member" | "admin";
    archived: boolean;
    hasLocalLogin: boolean;
    hasLinkedIdentity: boolean;
    invitePending: boolean;
    moviesAuthored: number;
    lastSeenAt?: string;
}

// The one-time claim URL returned by member-create, invite reissue, and restore.
// It is shown once and never resent, so the surface reveals it in a copy-or-lose
// ceremony rather than persisting it.
export interface InviteResult {
    claimUrl: string;
}

// The two invite states the admin overview renders. Used and revoked invites
// have no row: neither is actionable, and a revoked one is what Dismiss writes.
export type InviteStatus = "open" | "expired";

// One row of GET /invites: a member still waiting to set up a login, with their
// newest outstanding invite. `status` is the server's word, derived per read
// against its clock, so the surface never re-decides expiry locally. `issuedBy`
// is absent when the invite has no recorded issuer (a seeded invite, or an admin
// since deleted), and the line is dropped rather than inventing one. There is no
// claim URL and there cannot be: only the token's hash is stored, so an existing
// invite's link is gone for good and Regenerate is the only way to a new one.
export interface InviteSummary {
    id: number;
    memberId: number;
    memberName: string;
    status: InviteStatus;
    expiresAt: string;
    issuedAt: string;
    issuedBy?: string;
}

// Which of the two removal paths ran, so the surface can report "deleted" (gone,
// name freed) vs "archived" (restorable, attribution kept) after the same action.
export type RemoveOutcome = "deleted" | "archived";

export interface RemoveResult {
    outcome: RemoveOutcome;
}

// The session actor projected by GET /auth/me. username is null when the member
// has no local login; the two link-state flags are presence-derived server-side.
export interface MeResponse {
    id: number;
    displayName: string;
    username: string | null;
    role: "member" | "admin";
    hasLocalLogin: boolean;
    hasLinkedIdentity: boolean;
}

// One live session from GET /auth/sessions: the member's own devices, never
// anyone else's. `device` is derived server-side from the stored user agent
// ("Safari on iPhone"); `current` marks the session making the request, which is
// the one row that offers no sign-out. `ip` is omitted when none was recorded.
export interface SessionSummary {
    id: number;
    device: string;
    ip?: string;
    lastSeenAt: string;
    current: boolean;
}

// Public auth capabilities the unauthenticated login page reads to decide what
// to render. Today that is only whether an SSO provider is configured.
export interface AuthConfig {
    oidc: boolean;
}

// The two live claim modes GET /auth/claim/{token} returns for a valid invite:
// "placeholder" (set a fresh username + password) or "reset" (password only).
export type ClaimMode = "placeholder" | "reset";

// Drives the /claim/<token> page for a valid invite. The no-longer-valid and
// already-set-up terminal states arrive as 404/410 errors, not this shape.
export interface ClaimInfo {
    displayName: string;
    mode: ClaimMode;
    options: {
        password: boolean;
        oidc: boolean;
    };
}

// A selectable person in the Stats filter bar (actor, crew member, or adder).
export interface FilterPersonOption {
    id: number;
    name: string;
}

// Stats filter choices, derived server-side from the watched library — the
// shape consumed by FilterBar (mirrors the old client-side filterOptionsFrom).
export interface FilterOptionsResponse {
    genres: string[];
    actors: FilterPersonOption[];
    crew: FilterPersonOption[];
    years: number[];
    adders: FilterPersonOption[];
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
