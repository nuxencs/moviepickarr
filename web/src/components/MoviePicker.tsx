import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
    EyeIcon,
    FilmIcon,
    LinkIcon,
    LockIcon,
    LockOpenIcon,
    SearchIcon,
    ShuffleIcon,
} from "lucide-react";
import { useState } from "react";

import { APIClient } from "@/api/APIClient";
import {
    MoviesGetCurrentQueryOptions,
    MoviesGetPoolQueryOptions,
    MoviesGetWatchedQueryOptions,
    SettingsGetPoolLockQueryOptions,
} from "@/api/queries";
import { MoviesKeys, SettingsKeys, UsersKeys } from "@/api/query_keys";

import { AnimatedListItem } from "@/components/ui/animated-list";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { ScrollArea } from "@/components/ui/scroll-area";
import { toast } from "@/components/ui/toast";

export function MoviePicker() {
    const queryClient = useQueryClient();

    const { data: pooledMovies } = useQuery(MoviesGetPoolQueryOptions());
    const { data: currentMovie } = useQuery(MoviesGetCurrentQueryOptions());
    const { data: watchedMovies } = useQuery(MoviesGetWatchedQueryOptions());
    const { data: isPoolLocked } = useQuery(SettingsGetPoolLockQueryOptions());

    const pickMutation = useMutation({
        mutationFn: () => APIClient.movies.getRandom(),
        onSuccess: () => {
            toast.success("Movie picked successfully!");
            void queryClient.invalidateQueries({ queryKey: UsersKeys.list() });
            void queryClient.invalidateQueries({ queryKey: MoviesKeys.listpool() });
            void queryClient.invalidateQueries({ queryKey: MoviesKeys.current() });
        },
        onError: () => {
            toast.error("Failed to pick a random movie");
        },
    });

    const watchMutation = useMutation({
        mutationFn: () => APIClient.movies.markWatched(),
        onSuccess: () => {
            toast.success("Movie marked as watched!");
            void queryClient.invalidateQueries({ queryKey: MoviesKeys.current() });
            void queryClient.invalidateQueries({
                queryKey: MoviesKeys.listwatched(),
            });
        },
        onError: () => {
            toast.error("Failed to mark movie as watched");
        },
    });

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

    const filteredWatchedMovies = watchedMovies?.filter(
        (movie) =>
            movie.title.toLowerCase().includes(searchTerm.toLowerCase()) ||
            movie.addedByName.toLowerCase().includes(searchTerm.toLowerCase()),
    );
    const isSearching = searchTerm.length > 0;

    return (
        <div className="pr-4 pl-4 pb-4 p grid gap-4">
            <Card>
                <CardHeader>
                    <CardTitle className="flex items-center justify-between">
                        <span>Pooled Movies ({pooledMovies?.length})</span>
                        <div className="flex items-center space-x-2">
                            <Button
                                onClick={() => lockMutation.mutate()}
                                disabled={lockMutation.isPending}
                            >
                                {isPoolLocked ? <LockOpenIcon /> : <LockIcon />}
                                {isPoolLocked ? "Unlock Pool" : "Lock Pool"}
                            </Button>
                            <Button
                                onClick={() => pickMutation.mutate()}
                                disabled={
                                    pickMutation.isPending ||
                                    pooledMovies?.length === 0 ||
                                    currentMovie !== null
                                }
                            >
                                <ShuffleIcon className="mr-2" />
                                Pick Random Movie
                            </Button>
                        </div>
                    </CardTitle>
                </CardHeader>
                <CardContent>
                    {pooledMovies && pooledMovies.length > 0 ? (
                        <div className="grid gap-2">
                            {pooledMovies.map((movie) => (
                                <AnimatedListItem key={movie.movieID} id={movie.movieID}>
                                    <div className="flex items-center justify-between p-3 bg-gray-100 dark:bg-gray-800 rounded">
                                        <div className="flex items-center gap-2 overflow-hidden">
                                            <FilmIcon className="size-4 shrink-0" />
                                            <span className="truncate">
                                                {movie.title}
                                                <span className="pl-2 text-gray-400">
                                                    ({movie.addedByName})
                                                </span>
                                            </span>
                                        </div>
                                        <Button
                                            variant="ghost"
                                            size="icon"
                                            className="size-8"
                                            asChild
                                        >
                                            <a href={movie.link} target="_blank" rel="noopener noreferrer">
                                                <LinkIcon />
                                            </a>
                                        </Button>
                                    </div>
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

            <Card>
                <CardHeader>
                    <CardTitle className="flex items-center justify-between">
                        <span>Currently Selected Movie</span>
                        <Button
                            variant="default"
                            onClick={() => watchMutation.mutate()}
                            disabled={watchMutation.isPending || !currentMovie}
                        >
                            <EyeIcon className="mr-2" />
                            Mark as Watched
                        </Button>
                    </CardTitle>
                </CardHeader>
                <CardContent>
                    {currentMovie ? (
                        <div className="flex items-center justify-between p-3 bg-gray-100 dark:bg-gray-800 rounded">
                            <div className="flex items-center gap-2 overflow-hidden">
                                <FilmIcon className="size-4 shrink-0" />
                                <span className="truncate">
                                    {currentMovie.title}
                                    <span className="pl-2 text-gray-400">
                                        ({currentMovie.addedByName})
                                    </span>
                                </span>
                            </div>
                            <Button
                                variant="ghost"
                                size="icon"
                                className="size-8"
                                asChild
                            >
                                <a href={currentMovie.link} target="_blank" rel="noopener noreferrer">
                                    <LinkIcon />
                                </a>
                            </Button>
                        </div>
                    ) : (
                        <p className="text-gray-500 col-span-full text-center">
                            No movie selected
                        </p>
                    )}
                </CardContent>
            </Card>

            <Card>
                <CardHeader>
                    <CardTitle className="flex items-center justify-between">
                        <span>
                            Watched Movies (
                            {isSearching
                                ? `${filteredWatchedMovies?.length}/${watchedMovies?.length}`
                                : watchedMovies?.length}
                            )
                        </span>
                        <div className="flex items-center space-x-4">
                            <SearchIcon className="size-5 shrink-0 text-gray-400" />
                            <Input
                                placeholder="Search by title or user..."
                                value={searchTerm}
                                onChange={(e) => setSearchTerm(e.target.value)}
                                className="max-w-sm"
                            />
                        </div>
                    </CardTitle>
                </CardHeader>
                <CardContent className="pr-2">
                    {watchedMovies && watchedMovies.length > 0 ? (
                        <ScrollArea className="h-96 rounded-md">
                            <div className="grid gap-2 pr-4">
                                {filteredWatchedMovies &&
                                    filteredWatchedMovies.map((movie) => (
                                        <AnimatedListItem key={movie.movieID} id={movie.movieID}>
                                            <div className="flex items-center justify-between p-3 bg-gray-100 dark:bg-gray-800 rounded">
                                                <div className="flex items-center gap-2 overflow-hidden">
                                                    <FilmIcon className="size-4 shrink-0" />
                                                    <div className="flex flex-col">
                                                        <span className="truncate">
                                                            {movie.title}
                                                            <span className="pl-2 text-gray-400">
                                                                ({movie.addedByName})
                                                            </span>
                                                        </span>
                                                        <span className="text-sm text-gray-500">
                                                            Watched on:{" "}
                                                            {new Date(movie.watchedAt!).toLocaleDateString()}
                                                        </span>
                                                    </div>
                                                </div>
                                                <Button
                                                    variant="ghost"
                                                    size="icon"
                                                    className="size-8"
                                                    asChild
                                                >
                                                    <a href={movie.link} target="_blank" rel="noopener noreferrer">
                                                        <LinkIcon />
                                                    </a>
                                                </Button>
                                            </div>
                                        </AnimatedListItem>
                                    ))}
                            </div>
                        </ScrollArea>
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
