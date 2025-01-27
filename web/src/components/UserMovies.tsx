import React, {useState} from 'react';
import {Button} from '@/components/ui/button';
import {Film, Link, MoveDownIcon, MoveUpIcon, Trash2} from 'lucide-react';
import {AnimatedListItem} from '@/components/ui/animated-list';
import {AddMovie} from './AddMovie';
import {Card, CardContent, CardHeader, CardTitle} from '@/components/ui/card';
import {ConfirmDialog} from "@/components/ui/confirm-dialog";
import {Movie, User} from "@/types/Response";
import {useMutation, useQueryClient} from "@tanstack/react-query";
import {APIClient} from "@/api/APIClient";
import {toast} from "@/components/ui/toast";
import {MoviesKeys, UsersKeys} from "@/api/query_keys";

interface UserMoviesProps {
    user: User;
}

const MovieItem = ({movie, onDelete, onMove, moveIcon, disableMove}: {
    movie: Movie;
    onDelete: (movie: Movie) => void;
    onMove: () => void;
    moveIcon: React.ReactNode;
    disableMove?: boolean;
}) => (
    <div className="flex items-center justify-between py-1">
        <div className="flex items-center gap-2 overflow-hidden">
            <Film className="w-4 h-4 shrink-0"/>
            <span className="truncate">{movie.title}</span>
        </div>
        <div className="flex items-center gap-1 shrink-0">
            <Button
                variant="ghost"
                size="icon"
                className="h-8 w-8"
                asChild
            >
                <a href={movie.link} target="_blank" rel="noopener noreferrer">
                    <Link className="w-4 h-4"/>
                </a>
            </Button>
            <Button
                variant="ghost"
                size="icon"
                className="h-8 w-8"
                onClick={onMove}
                disabled={disableMove}
            >
                {moveIcon}
            </Button>
            <Button
                variant="ghost"
                size="icon"
                className="h-8 w-8"
                onClick={() => onDelete(movie)}
            >
                <Trash2 className="w-4 h-4"/>
            </Button>
        </div>
    </div>
);

export function UserMovies({user}: UserMoviesProps) {
    const queryClient = useQueryClient();

    const [movieToDelete, setMovieToDelete] = useState<Movie | null>(null);
    const isPoolFull = Object.keys(user.currentPool).length >= 3;

    const deleteMutation = useMutation({
        mutationFn: (movie: Movie) => {
            return APIClient.users.deleteMovie(user.userID, movie.movieID);
        },
        onSuccess: () => {
            toast.success(`Movie ${deleteMutation.variables?.title} deleted!`);
            setMovieToDelete(null);
            void queryClient.invalidateQueries({queryKey: UsersKeys.list()})
            void queryClient.invalidateQueries({queryKey: MoviesKeys.listpool()})
        },
        onError: () => {
            toast.error(`Error deleting movie`);
            setMovieToDelete(null);
        }
    })

    const onConfirm = () => {
        if (movieToDelete) {
            deleteMutation.mutate(movieToDelete)
        }
    }

    const moveMutation = useMutation({
        mutationFn: (movieID: string) => APIClient.users.moveMovie(user.userID, movieID),
        onSuccess: () => {
            void queryClient.invalidateQueries({queryKey: UsersKeys.list()})
            void queryClient.invalidateQueries({queryKey: MoviesKeys.listpool()})
        },
        onError: () => {
            toast.error(`Error moving movie`);
        }
    })

    return (
        <div className="space-y-4">
            <AddMovie
                userID={user.userID}
            />

            <Card>
                <CardHeader className="pb-2">
                    <CardTitle className="text-sm">
                        Pool ({Object.keys(user.currentPool).length}/3)
                    </CardTitle>
                </CardHeader>
                <CardContent>
                    <div className="space-y-1">
                        {Object.keys(user.currentPool).length > 0 ? (
                            Object.values(user.currentPool).map((movie) => (
                                <AnimatedListItem key={movie.movieID} id={movie.movieID}>
                                    <MovieItem
                                        movie={movie}
                                        onDelete={setMovieToDelete}
                                        onMove={() => moveMutation.mutate(movie.movieID)}
                                        moveIcon={<MoveDownIcon className="w-4 h-4"/>}
                                    />
                                </AnimatedListItem>
                            ))
                        ) : (
                            <p className="text-gray-500 col-span-full text-center">No movies in personal pool</p>
                        )}
                    </div>
                </CardContent>
            </Card>

            <Card>
                <CardHeader className="pb-2">
                    <CardTitle className="text-sm">
                        Stash ({Object.keys(user.stash).length})
                    </CardTitle>
                </CardHeader>
                <CardContent>
                    <div className="space-y-1">
                        {Object.keys(user.stash).length > 0 ? (
                            Object.values(user.stash).map((movie) => (
                                <AnimatedListItem key={movie.movieID} id={movie.movieID}>
                                    <MovieItem
                                        movie={movie}
                                        onDelete={setMovieToDelete}
                                        onMove={() => moveMutation.mutate(movie.movieID)}
                                        moveIcon={<MoveUpIcon className="w-4 h-4"/>}
                                        disableMove={isPoolFull}
                                    />
                                </AnimatedListItem>
                            ))
                        ) : (
                            <p className="text-gray-500 col-span-full text-center">No movies in personal stash</p>
                        )}
                    </div>
                </CardContent>
            </Card>

            <ConfirmDialog
                isOpen={!!movieToDelete}
                onClose={() => setMovieToDelete(null)}
                onConfirm={onConfirm}
                title="Delete Movie"
                description={`Are you sure you want to delete ${movieToDelete?.title}? This action cannot be undone.`}
                confirmText="Delete"
                cancelText="Cancel"
            />
        </div>
    );
}
