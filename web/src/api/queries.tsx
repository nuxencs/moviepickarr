import { keepPreviousData, queryOptions } from "@tanstack/react-query";

import { APIClient } from "@/api/APIClient";
import { MoviesKeys, SettingsKeys, StatsKeys, UsersKeys } from "@/api/query_keys";

import type { StatsWindow } from "@/types/Response";

export const UsersGetAllQueryOptions = () =>
  queryOptions({
    queryKey: UsersKeys.list(),
    queryFn: () => APIClient.users.getAll(),
    refetchOnWindowFocus: false
  })

export const UsersGetPoolQueryOptions = (userID: number) =>
  queryOptions({
    queryKey: UsersKeys.pool(),
    queryFn: () => APIClient.users.getPool(userID),
    refetchOnWindowFocus: false
  })

export const UsersGetStashQueryOptions = (userID: number) =>
  queryOptions({
    queryKey: UsersKeys.stash(),
    queryFn: () => APIClient.users.getStash(userID),
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

/** Stats filter choices (genres/actors/crew/years/pickers), derived server-side
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

export const SettingsGetNextPickerQueryOptions = () =>
  queryOptions({
    queryKey: SettingsKeys.nextPicker(),
    queryFn: () => APIClient.settings.getNextPicker(),
    refetchOnWindowFocus: false
  })

/** Sorted, comma-joined person ids — the canonical form shared by the query
 *  key and the request, mirroring the backend's cache-key canonicalization. */
const idListKey = (ids?: number[]) =>
  ids && ids.length > 0 ? [...ids].sort((a, b) => a - b).join(",") : undefined;

export const StatsGetQueryOptions = (
  window: StatsWindow,
  timezone: string,
  start?: string,
  end?: string,
  genre?: string,
  actorIds?: number[],
  crewIds?: number[],
  addedByIds?: number[],
  releaseYear?: number,
  decade?: number,
) => {
  const actorsKey = idListKey(actorIds);
  const crewKey = idListKey(crewIds);
  const addedByKey = idListKey(addedByIds);
  return queryOptions({
    queryKey: StatsKeys.byWindow(window, timezone, start, end, genre, actorsKey, crewKey, addedByKey, releaseYear, decade),
    queryFn: ({ signal }) =>
      APIClient.stats.get({ window, timezone, start, end, genre, actorIds: actorsKey, crewIds: crewKey, addedByIds: addedByKey, releaseYear, decade }, signal),
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
