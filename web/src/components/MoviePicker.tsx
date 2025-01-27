import {useState} from 'react';
import {Button} from '@/components/ui/button';
import {Eye, Film, Link, Search, Shuffle} from 'lucide-react';
import {Input} from '@/components/ui/input';
import {AnimatedListItem} from '@/components/ui/animated-list';
import {Card, CardContent, CardHeader, CardTitle} from '@/components/ui/card';
import {ScrollArea} from "@/components/ui/scroll-area";
import {useMutation, useQuery, useQueryClient} from "@tanstack/react-query";
import {MoviesGetCurrentQueryOptions, MoviesGetPoolQueryOptions, MoviesGetWatchedQueryOptions} from "@/api/queries";
import {APIClient} from "@/api/APIClient";
import {toast} from "@/components/ui/toast";
import {MoviesKeys, UsersKeys} from "@/api/query_keys";

export function MoviePicker() {
    const queryClient = useQueryClient();

    const {data: pooledMovies} = useQuery(MoviesGetPoolQueryOptions())
    const {data: currentMovie} = useQuery(MoviesGetCurrentQueryOptions())
    const {data: watchedMovies} = useQuery(MoviesGetWatchedQueryOptions())

    const pickMutation = useMutation({
        mutationFn: () => APIClient.movies.getRandom(),
        onSuccess: () => {
            toast.success('Movie picked successfully!');
            void queryClient.invalidateQueries({queryKey: UsersKeys.list()});
            void queryClient.invalidateQueries({queryKey: MoviesKeys.listpool()});
            void queryClient.invalidateQueries({queryKey: MoviesKeys.current()});
        },
        onError: () => {
            toast.error('Failed to pick a random movie')
        }
    })

    const watchMutation = useMutation({
        mutationFn: () => APIClient.movies.markWatched(),
        onSuccess: () => {
            toast.success('Movie marked as watched!');
            void queryClient.invalidateQueries({queryKey: MoviesKeys.current()});
            void queryClient.invalidateQueries({queryKey: MoviesKeys.listwatched()});
        },
        onError: () => {
            toast.error('Failed to mark movie as watched')
        }
    })

    const [searchTerm, setSearchTerm] = useState('');

    const filteredWatchedMovies = watchedMovies?.filter(movie =>
        movie.title.toLowerCase().includes(searchTerm.toLowerCase()) ||
        movie.addedByName.toLowerCase().includes(searchTerm.toLowerCase())
    );
    const isSearching = searchTerm.length > 0;

    return (
        <div className="pr-4 pl-4 pb-4 p grid gap-4">
            <Card>
                <CardHeader>
                    <CardTitle className="flex items-center justify-between">
                        <span>Pooled Movies ({pooledMovies?.length})</span>
                        <Button
                            onClick={() => pickMutation.mutate()}
                            disabled={pickMutation.isPending || pooledMovies?.length === 0 || currentMovie !== null}
                        >
                            <Shuffle className="mr-2"/>
                            Pick Random Movie
                        </Button>
                    </CardTitle>
                </CardHeader>
                <CardContent>
                    {pooledMovies && pooledMovies.length > 0 ? (
                        <div className="grid gap-2">
                            {pooledMovies.map((movie) => (
                                <AnimatedListItem key={movie.movieID} id={movie.movieID}>
                                    <div
                                        className="flex items-center justify-between p-3 bg-gray-100 dark:bg-gray-800 rounded">
                                        <div className="flex items-center gap-2 overflow-hidden">
                                            <Film className="w-4 h-4 shrink-0"/>
                                            <span className="truncate">
                                                {movie.title}
                                                <span
                                                    className="pl-2 text-gray-400">({movie.addedByName})
                                                </span>
                                            </span>
                                        </div>
                                        <Button
                                            variant="ghost"
                                            size="icon"
                                            className="w-8 h-8"
                                            asChild
                                        >
                                            <a href={movie.link} target="_blank" rel="noopener noreferrer">
                                                <Link/>
                                            </a>
                                        </Button>
                                    </div>
                                </AnimatedListItem>
                            ))}
                        </div>
                    ) : (
                        <p className="text-gray-500 col-span-full text-center">No movies in the pool</p>
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
                            <Eye className="w-4 h-4 mr-2"/>
                            Mark as Watched
                        </Button>
                    </CardTitle>
                </CardHeader>
                <CardContent>
                    {currentMovie ? (
                        <div className="flex items-center justify-between p-3 bg-gray-100 dark:bg-gray-800 rounded">
                            <div className="flex items-center gap-2 overflow-hidden">
                                <Film className="w-4 h-4 shrink-0"/>
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
                                className="w-8 h-8"
                                asChild
                            >
                                <a href={currentMovie.link} target="_blank" rel="noopener noreferrer">
                                    <Link className="w-4 h-4"/>
                                </a>
                            </Button>
                        </div>
                    ) : (
                        <p className="text-gray-500 col-span-full text-center">No movie selected</p>
                    )}
                </CardContent>
            </Card>

            <Card>
                <CardHeader>
                    <CardTitle className="flex items-center justify-between">
                        <span>Watched Movies
                            {filteredWatchedMovies && isSearching ? (
                                <span> ({filteredWatchedMovies.length}/{watchedMovies?.length})</span>
                            ) : (
                                <span> ({watchedMovies?.length})</span>
                            )}
                        </span>
                        <div className="flex items-center space-x-2">
                            <Search className="w-4 h-4 text-gray-500"/>
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
                                {filteredWatchedMovies && filteredWatchedMovies.map((movie) => (
                                        <AnimatedListItem key={movie.movieID} id={movie.movieID}>
                                            <div
                                                className="flex items-center justify-between p-3 bg-gray-100 dark:bg-gray-800 rounded">
                                                <div className="flex items-center gap-2 overflow-hidden">
                                                    <Film className="w-4 h-4 shrink-0"/>
                                                    <div className="flex flex-col">
                                                        <span className="truncate">
                                                            {movie.title}
                                                            <span
                                                                className="pl-2 text-gray-400">({movie.addedByName})
                                                            </span>
                                                        </span>
                                                        <span className="text-sm text-gray-500">
                                                Watched on: {new Date(movie.watchedAt!).toLocaleDateString()}
                                                        </span>
                                                    </div>
                                                </div>
                                                <Button
                                                    variant="ghost"
                                                    size="icon"
                                                    className="w-8 h-8"
                                                    asChild
                                                >
                                                    <a href={movie.link} target="_blank" rel="noopener noreferrer">
                                                        <Link className="w-4 h-4"/>
                                                    </a>
                                                </Button>
                                            </div>
                                        </AnimatedListItem>
                                    )
                                )}
                            </div>
                        </ScrollArea>
                    ) : (
                        <p className="text-gray-500 col-span-full text-center">No movies watched</p>
                    )}
                </CardContent>
            </Card>
        </div>
    );
}
