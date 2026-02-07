import { queryOptions } from "@tanstack/react-query";

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

export const StatsGetQueryOptions = (
  window: StatsWindow,
  timezone: string,
  start?: string,
  end?: string,
) =>
  queryOptions({
    queryKey: StatsKeys.byWindow(window, timezone, start, end),
    queryFn: () => APIClient.stats.get({ window, timezone, start, end }),
    refetchOnWindowFocus: false,
    staleTime: 60_000,
    gcTime: 600_000,
  })
