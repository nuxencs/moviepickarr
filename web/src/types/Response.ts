// `character` is set on cast entries, `job` on crew entries.
export interface CreditPerson {
    id: number;
    name: string;
    profilePath?: string;
    character?: string;
    job?: string;
}

// Mirrors the server's domain.MovieStatus.
export type MovieStatus = "pool" | "stash" | "current" | "wildcard" | "watched";

// Derived from MovieStatus so renaming a status breaks here too.
export type MoveTarget = Extract<MovieStatus, "pool" | "stash">;

// Lean list wire class (pool, watched, board reads); modal fields need a detail fetch.
export interface MovieTile {
    movieID: number;
    title: string;
    link: string;
    addedAt: string;
    addedByID: number;
    addedByName: string;
    // Archived adders keep attribution but have no board. Omitted for active adders.
    addedByArchived?: boolean;
    watchedAt?: string;
    // Set on a watched wildcard: the Current draw it detoured from.
    wildcardOfMovieId?: number;

    tmdbId?: number;
    imdbId?: string;

    // Optional while enrichment is pending.
    posterPath?: string;
    releaseDate?: string;
    runtime?: number;
    genres?: string[];
    voteAverage?: number;
}

// Full wire class. A held winner stays projected as pooled until reveal.
export interface MovieDetail extends MovieTile {
    status: MovieStatus;

    // Only on /movies/current and movie:drawn. revealAt is the server's auto-reveal
    // deadline; serverNow lets the client time the spin without trusting its own clock.
    drawnAt?: string;
    revealAt?: string;
    serverNow?: string;
    // Only the drawing client shows the confirm button. revealed lets a reload after
    // the reveal skip the reel (see drawSpin).
    drawClientId?: string;
    revealed?: boolean;
    // Modal-only, optional while enrichment is pending.
    backdropPath?: string;
    tagline?: string;
    overview?: string;

    // Cast in billing order, crew whitelisted jobs only; omitted when empty.
    cast?: CreditPerson[];
    crew?: CreditPerson[];
}

// Adds a lean reel source to the winning record. The recovery broadcast may omit
// candidates; clients then skip the reel.
export interface MovieDrawPayload extends MovieDetail {
    candidates?: MovieTile[];
}

// The one Active wildcard; it takes over the Hero while it holds the Current draw.
export interface Wildcard {
    id: number;
    hostMovieId: number;
    selectedAt: string;
    movie: MovieDetail;
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
    // Server-owned freeze during an unrevealed draw, independent of any local reel.
    drawInProgress: boolean;
}

export type MemberRole = "member" | "guest" | "admin";

// Login state is presence-derived server-side, never a stored flag. moviesAuthored
// tells the surface whether a remove deletes or archives.
export interface RosterMember {
    id: number;
    name: string;
    username?: string;
    role: MemberRole;
    archived: boolean;
    hasLocalLogin: boolean;
    hasLinkedIdentity: boolean;
    invitePending: boolean;
    moviesAuthored: number;
    lastSeenAt?: string;
}

// Shown once and never resent, so the surface does not persist it.
export interface InviteResult {
    claimUrl: string;
}

// Used and revoked invites have no row: neither is actionable.
export type InviteStatus = "open" | "expired";

// `status` is derived against the response's serverNow. The claim URL cannot be
// recovered (only its token hash is stored); replacement reveals a new link.
export interface InviteSummary {
    id: string;
    memberId: number;
    memberName: string;
    status: InviteStatus;
    expiresAt: string;
    issuedAt: string;
    issuedBy?: string;
}

export interface InvitesResponse {
    serverNow: string;
    items: InviteSummary[];
}

// "deleted" frees the name; "archived" is restorable and keeps attribution.
export type RemoveOutcome = "deleted" | "archived";

export interface RemoveResult {
    outcome: RemoveOutcome;
}

// username is null without a local login.
export interface MeResponse {
    id: number;
    displayName: string;
    username: string | null;
    role: MemberRole;
    hasLocalLogin: boolean;
    hasLinkedIdentity: boolean;
}

// The member's own sessions only. `id` is a public handle, never the store row id.
export interface SessionSummary {
    id: string;
    device: string;
    lastSeenAt: string;
    current: boolean;
}

export interface AuthConfig {
    oidc: boolean;
}

// "placeholder" sets username + password; "reset" sets the password only.
export type ClaimMode = "placeholder" | "reset";

// No-longer-valid and already-set-up arrive as 404/410 errors, not this shape.
export interface ClaimInfo {
    displayName: string;
    mode: ClaimMode;
    options: {
        password: boolean;
        oidc: boolean;
    };
}

export interface FilterPersonOption {
    id: number;
    name: string;
}

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

export interface StatsFilterPerson {
    personId: number;
    name?: string;
}

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
    // The client joins these to the cached watched list for the in-window rail.
    matchedMovieIDs: number[];
    timezone: string;
    totalWatched: number;
    countsByWindow: StatsWindowCount[];
    watchedByUser: StatsNamedCount[];
    weekdayActivity: StatsNamedCount[];
    hourActivity: StatsHourCount[];
    customRangeStart?: string;
    customRangeEnd?: string;

    // Computed over the filtered in-window subset.
    topGenres: StatsNamedCount[];
    topDirectors: StatsPersonCount[];
    topActors: StatsPersonCount[];
    releaseYears: StatsYearCount[];
    runtime: StatsRuntime;
    averageRating: number;
    filters: StatsFiltersEcho;
}
