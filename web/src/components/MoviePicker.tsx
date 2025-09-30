import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { LockIcon, LockOpenIcon, SearchIcon, } from "lucide-react";
import { useMemo, useState } from "react";

import { APIClient } from "@/api/APIClient";
import {
    MoviesGetPoolQueryOptions,
    MoviesGetWatchedQueryOptions,
    SettingsGetPoolLockQueryOptions,
} from "@/api/queries";
import { SettingsKeys } from "@/api/query_keys";

import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { toast } from "@/components/ui/toast";
import { MovieItem } from "@/components/MovieItem";
import { AnimatedListItem } from "@/components/ui/animated-list.tsx";

export function MoviePicker() {
    const queryClient = useQueryClient();

    const { data: pooledMovies } = useQuery(MoviesGetPoolQueryOptions());
    const { data: watchedMovies } = useQuery(MoviesGetWatchedQueryOptions());
    const { data: isPoolLocked } = useQuery(SettingsGetPoolLockQueryOptions());

    const lockMutation = useMutation({
        mutationFn: () => {
            return APIClient.settings.toggleLock(!isPoolLocked);
        },
        onSuccess: () => {
            void queryClient.invalidateQueries({ queryKey: SettingsKeys.poolLock() });
        },
        onError: () => {
            toast.error(`Failed to toggle the pool lock`);
        },
    });

    const [searchTerm, setSearchTerm] = useState("");

    const filteredWatchedMovies = useMemo(() =>
        watchedMovies?.filter(
            (movie) =>
                movie.title.toLowerCase().includes(searchTerm.toLowerCase()) ||
                movie.addedByName.toLowerCase().includes(searchTerm.toLowerCase())
        ), [watchedMovies, searchTerm]);
    const isSearching = searchTerm.length > 0;

    return (
        <div className="p-4 pt-0 grid gap-4">
            <Card>
                <CardHeader>
                    <CardTitle className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
                        <span className="shrink-0">Pooled Movies ({pooledMovies?.length})</span>
                        <Button
                            onClick={() => lockMutation.mutate()}
                            disabled={lockMutation.isPending}
                            className="w-full sm:w-auto"
                        >
                            {isPoolLocked ? <LockOpenIcon/> : <LockIcon/>}
                            {isPoolLocked ? "Unlock Pool" : "Lock Pool"}
                        </Button>
                    </CardTitle>
                </CardHeader>
                <CardContent>
                    {pooledMovies && pooledMovies.length > 0 ? (
                        <div className="grid gap-2">
                            {pooledMovies.map((movie) => (
                                <AnimatedListItem
                                    key={movie.movieID}
                                    id={movie.movieID}
                                    className="truncate"
                                >
                                    <MovieItem key={movie.movieID} movie={movie}/>
                                </AnimatedListItem>
                            ))}
                        </div>
                    ) : (
                        <p className="text-gray-500 col-span-full text-center">
                            No movies in the pool
                        </p>
                    )}
                </CardContent>
            </Card>

            <Card className="overflow-hidden">
                <CardHeader>
                    <CardTitle className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
                        <span className="shrink-0">
                            Watched Movies (
                            {isSearching
                                ? `${filteredWatchedMovies?.length}/${watchedMovies?.length}`
                                : watchedMovies?.length}
                            )
                        </span>
                        <div className="flex items-center gap-4">
                            <SearchIcon className="size-5 shrink-0 text-gray-400"/>
                            <Input
                                placeholder="Search by title or user..."
                                value={searchTerm}
                                onChange={(e) => setSearchTerm(e.target.value)}
                                className="w-full text-sm"
                            />
                        </div>
                    </CardTitle>
                </CardHeader>
                <CardContent className="pr-2">
                    {watchedMovies && watchedMovies.length > 0 ? (
                        <div className="space-y-2 pr-4 max-h-96 rounded-md overflow-auto">
                            {filteredWatchedMovies &&
                                filteredWatchedMovies.map((movie) => (
                                    <MovieItem key={movie.movieID} movie={movie} watched={true}/>
                                ))}
                        </div>
                    ) : (
                        <p className="text-gray-500 col-span-full text-center">
                            No movies watched
                        </p>
                    )}
                </CardContent>
            </Card>
        </div>
    );
}
