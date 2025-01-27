import {queryOptions} from "@tanstack/react-query";
import {MoviesKeys, UsersKeys} from "@/api/query_keys";
import {APIClient} from "@/api/APIClient.ts";

export const UsersGetAllQueryOptions = () =>
    queryOptions({
        queryKey: UsersKeys.list(),
        queryFn: () => APIClient.users.getAll(),
        refetchOnWindowFocus: false
    })

export const UsersGetPoolQueryOptions = (userID: string) =>
    queryOptions({
        queryKey: UsersKeys.pool(),
        queryFn: () => APIClient.users.getPool(userID),
        refetchOnWindowFocus: false
    })

export const UsersGetStashQueryOptions = (userID: string) =>
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
