import { keepPreviousData, queryOptions } from "@tanstack/react-query";

import { APIClient } from "@/api/APIClient";
import { AuthKeys, InvitesKeys, MoviesKeys, SettingsKeys, StatsKeys, UsersKeys } from "@/api/query_keys";

import type { StatsFilters } from "@/components/moviepickarr/statsSearch";

import type { StatsWindow } from "@/types/Response";

export const AuthConfigQueryOptions = () =>
  queryOptions({
    queryKey: AuthKeys.config(),
    queryFn: () => APIClient.auth.config(),
    staleTime: 300_000,
  })

export const PosterWallQueryOptions = () =>
  queryOptions({
    queryKey: AuthKeys.posterWall(),
    queryFn: () => APIClient.auth.posterWall(),
    staleTime: 300_000,
  })

/** No retry: a 401 means "not logged in", and retrying delays the login redirect. */
export const MeQueryOptions = () =>
  queryOptions({
    queryKey: AuthKeys.me(),
    queryFn: () => APIClient.auth.me(),
    retry: false,
  })

/** No SSE event covers session changes, so refetch on mount and focus. */
export const SessionsQueryOptions = () =>
  queryOptions({
    queryKey: AuthKeys.sessions(),
    queryFn: () => APIClient.auth.sessions(),
    staleTime: 0,
    refetchOnWindowFocus: true,
    retry: false,
  })

/** No retry: 404/410 are terminal answers. */
export const ClaimQueryOptions = (token: string) =>
  queryOptions({
    queryKey: AuthKeys.claim(token),
    queryFn: () => APIClient.auth.validateClaim(token),
    retry: false,
  })

export const UsersGetAllQueryOptions = () =>
  queryOptions({
    queryKey: UsersKeys.list(),
    queryFn: () => APIClient.board.getAll(),
  })

/** No retry: a 403 is the "Admins only" answer the page renders. */
export const RosterQueryOptions = () =>
  queryOptions({
    queryKey: UsersKeys.roster(),
    queryFn: () => APIClient.members.roster(),
    retry: false,
  })

/**
 * Refetch on mount and focus: there is deliberately no `invite:claimed` event,
 * because the SSE stream is not role-filtered. A 403 is "Admins only", no retry.
 */
export const InvitesQueryOptions = () =>
  queryOptions({
    queryKey: InvitesKeys.list(),
    queryFn: () => APIClient.invites.list(),
    staleTime: 0,
    refetchOnWindowFocus: true,
    retry: false,
  })

export const UsersGetPoolQueryOptions = (userID: number) =>
  queryOptions({
    queryKey: UsersKeys.pool(),
    queryFn: () => APIClient.board.getPool(userID),
  })

export const UsersGetStashQueryOptions = (userID: number) =>
  queryOptions({
    queryKey: UsersKeys.stash(),
    queryFn: () => APIClient.board.getStash(userID),
  })

export const MoviesGetPoolQueryOptions = () =>
  queryOptions({
    queryKey: MoviesKeys.listpool(),
    queryFn: () => APIClient.movies.getPooled(),
  })

export const MoviesGetCurrentQueryOptions = () =>
  queryOptions({
    queryKey: MoviesKeys.current(),
    queryFn: () => APIClient.movies.getCurrent(),
  })

export const MoviesGetWildcardQueryOptions = () =>
  queryOptions({
    queryKey: MoviesKeys.wildcard(),
    queryFn: () => APIClient.movies.getWildcard(),
  })

export const MoviesGetWatchedQueryOptions = () =>
  queryOptions({
    queryKey: MoviesKeys.listwatched(),
    queryFn: () => APIClient.movies.getWatched(),
  })

export const MovieDetailQueryOptions = (movieID: number) =>
  queryOptions({
    queryKey: MoviesKeys.detail(movieID),
    queryFn: ({ signal }) => APIClient.movies.get(movieID, signal),
    // A removed record cannot reappear by retrying the same id.
    retry: (failureCount, error) =>
      (error as { status?: unknown } | null)?.status !== 404 && failureCount < 3,
  })

export const FilterOptionsQueryOptions = () =>
  queryOptions({
    queryKey: MoviesKeys.filterOptions(),
    queryFn: ({ signal }) => APIClient.movies.getFilterOptions(signal),
    staleTime: 300_000,
  })

export const SettingsGetPoolStateQueryOptions = (enabled = true) =>
  queryOptions({
    queryKey: SettingsKeys.poolLock(),
    queryFn: () => APIClient.settings.getPoolState(),
    enabled,
  })

export const SettingsGetNextUpQueryOptions = () =>
  queryOptions({
    queryKey: SettingsKeys.nextUp(),
    queryFn: () => APIClient.settings.getNextUp(),
  })

/** Sorted, comma-joined ids, mirroring the backend's cache-key canonicalization. */
const idListKey = (ids?: number[]) =>
  ids && ids.length > 0 ? [...ids].sort((a, b) => a - b).join(",") : undefined;

export const StatsGetQueryOptions = (
  window: StatsWindow,
  timezone: string,
  range: { start?: string; end?: string },
  filters: StatsFilters,
) => {
  // Key and request share this one serialization, so they cannot disagree.
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
    // Keeps numbers mounted so NumberFlow rolls them instead of blanking to loading.
    placeholderData: keepPreviousData,
    staleTime: 60_000,
    gcTime: 600_000,
  });
}
