import { keepPreviousData, queryOptions } from "@tanstack/react-query";

import { APIClient } from "@/api/APIClient";
import { AuthKeys, MoviesKeys, SettingsKeys, StatsKeys, UsersKeys } from "@/api/query_keys";

import type { StatsFilters } from "@/components/moviepickarr/statsSearch";

import type { StatsWindow } from "@/types/Response";

/** The public SSO-presence fact the login page reads. Rarely changes (it's an
 *  operator config), so a generous staleTime; no session needed. */
export const AuthConfigQueryOptions = () =>
  queryOptions({
    queryKey: AuthKeys.config(),
    queryFn: () => APIClient.auth.config(),
    refetchOnWindowFocus: false,
    staleTime: 300_000,
  })

/** The public poster wall (popularity-ordered TMDB poster paths) the login
 *  screen renders behind the form. Public artwork that rarely changes and needs
 *  no session, so a generous staleTime and no refetch on focus; an empty or
 *  errored fetch just leaves the gradient stand-ins in place. */
export const PosterWallQueryOptions = () =>
  queryOptions({
    queryKey: AuthKeys.posterWall(),
    queryFn: () => APIClient.auth.posterWall(),
    refetchOnWindowFocus: false,
    staleTime: 300_000,
  })

/** The current session actor. A 401 is an expected "not logged in" answer, so
 *  it must not retry (retrying a 401 just delays the login redirect). */
export const MeQueryOptions = () =>
  queryOptions({
    queryKey: AuthKeys.me(),
    queryFn: () => APIClient.auth.me(),
    refetchOnWindowFocus: false,
    retry: false,
  })

/** Claim-page data for a token. 404/410 (no-longer-valid / already-set-up) are
 *  terminal answers, not transient failures, so it must not retry. */
export const ClaimQueryOptions = (token: string) =>
  queryOptions({
    queryKey: AuthKeys.claim(token),
    queryFn: () => APIClient.auth.validateClaim(token),
    refetchOnWindowFocus: false,
    retry: false,
  })

export const UsersGetAllQueryOptions = () =>
  queryOptions({
    queryKey: UsersKeys.list(),
    queryFn: () => APIClient.board.getAll(),
    refetchOnWindowFocus: false
  })

/** The admin roster (presence-derived login state per member). A 403 is the
 *  "Admins only" answer the roster page renders as a forbidden state, not an
 *  error to retry, so it must not retry. */
export const RosterQueryOptions = () =>
  queryOptions({
    queryKey: UsersKeys.roster(),
    queryFn: () => APIClient.members.roster(),
    refetchOnWindowFocus: false,
    retry: false,
  })

export const UsersGetPoolQueryOptions = (userID: number) =>
  queryOptions({
    queryKey: UsersKeys.pool(),
    queryFn: () => APIClient.board.getPool(userID),
    refetchOnWindowFocus: false
  })

export const UsersGetStashQueryOptions = (userID: number) =>
  queryOptions({
    queryKey: UsersKeys.stash(),
    queryFn: () => APIClient.board.getStash(userID),
    refetchOnWindowFocus: false
  })

export const MoviesGetPoolQueryOptions = () =>
  queryOptions({
    queryKey: MoviesKeys.listpool(),
    queryFn: () => APIClient.movies.getPooled(),
    refetchOnWindowFocus: false
  })

export const MoviesGetCurrentQueryOptions = () =>
  queryOptions({
    queryKey: MoviesKeys.current(),
    queryFn: () => APIClient.movies.getCurrent(),
    refetchOnWindowFocus: false
  })

export const MoviesGetWatchedQueryOptions = () =>
  queryOptions({
    queryKey: MoviesKeys.listwatched(),
    queryFn: () => APIClient.movies.getWatched(),
    refetchOnWindowFocus: false
  })

/** Full enriched record (cast/crew/overview) for the detail modal — lazy-loaded
 *  on open, since the list payloads ship lean. */
export const MovieDetailQueryOptions = (movieID: number) =>
  queryOptions({
    queryKey: MoviesKeys.detail(movieID),
    queryFn: ({ signal }) => APIClient.movies.get(movieID, signal),
    refetchOnWindowFocus: false,
  })

/** Stats filter choices (genres/actors/crew/years/adders), derived server-side
 *  from the watched library. Changes only on watch/edit/enrich/user events, so a
 *  generous staleTime — SSE invalidation refreshes it when it actually changes. */
export const FilterOptionsQueryOptions = () =>
  queryOptions({
    queryKey: MoviesKeys.filterOptions(),
    queryFn: ({ signal }) => APIClient.movies.getFilterOptions(signal),
    refetchOnWindowFocus: false,
    staleTime: 300_000,
  })

export const SettingsGetPoolLockQueryOptions = () =>
  queryOptions({
    queryKey: SettingsKeys.poolLock(),
    queryFn: () => APIClient.settings.getLock(),
    refetchOnWindowFocus: false
  })

export const SettingsGetNextUpQueryOptions = () =>
  queryOptions({
    queryKey: SettingsKeys.nextUp(),
    queryFn: () => APIClient.settings.getNextUp(),
    refetchOnWindowFocus: false
  })

/** Sorted, comma-joined person ids — the canonical form shared by the query
 *  key and the request, mirroring the backend's cache-key canonicalization. */
const idListKey = (ids?: number[]) =>
  ids && ids.length > 0 ? [...ids].sort((a, b) => a - b).join(",") : undefined;

export const StatsGetQueryOptions = (
  window: StatsWindow,
  timezone: string,
  range: { start?: string; end?: string },
  filters: StatsFilters,
) => {
  // The ONE serialization of the filter value: the query key and the request
  // both read it, so a cache entry can never disagree with what was fetched.
  const canonical = {
    genre: filters.genre,
    actorIds: idListKey(filters.actorIds),
    crewIds: idListKey(filters.crewIds),
    addedByIds: idListKey(filters.addedByIds),
    releaseYear: filters.releaseYear,
    decade: filters.decade,
  };
  return queryOptions({
    queryKey: StatsKeys.byWindow(window, timezone, range.start, range.end, canonical),
    queryFn: ({ signal }) =>
      APIClient.stats.get({ window, timezone, start: range.start, end: range.end, ...canonical }, signal),
    // Keep the previous window/filter's result on screen while the next one loads,
    // so the stats body never blanks to "Loading stats…" on an uncached change —
    // the numbers stay mounted and roll (NumberFlow) to the new values instead of
    // remounting static. The first-ever load still shows the loading placeholder.
    placeholderData: keepPreviousData,
    refetchOnWindowFocus: false,
    staleTime: 60_000,
    gcTime: 600_000,
  });
}
