import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { FilmIcon, LinkIcon, MoveDownIcon, MoveUpIcon, Trash2Icon } from 'lucide-react';
import React from 'react';

import { APIClient } from "@/api/APIClient";
import { SettingsGetPoolLockQueryOptions } from "@/api/queries";
import { MoviesKeys, UsersKeys } from "@/api/query_keys";

import { AddMovie } from '@/components/AddMovie';

import { AnimatedListItem } from '@/components/ui/animated-list';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { DeletionDialog } from "@/components/ui/deletion-dialog";
import { toast } from "@/components/ui/toast";

import { useToggle } from "@/hooks/hooks";
import { Movie, User } from "@/types/Response";

interface UserMoviesProps {
    user: User;
}

interface MovieItemProps {
    user: User;
    movie: Movie;
    moveIcon: React.ReactNode;
    disableMove?: boolean;
    disableDelete?: boolean;
}

export function UserMovies({ user }: UserMoviesProps) {
    const { data: isPoolLocked } = useQuery(SettingsGetPoolLockQueryOptions())

    const isPoolFull = Object.keys(user.currentPool).length >= 3;

    return (
        <div className="space-y-4">
            <AddMovie userID={user.userID} />

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
                                        user={user}
                                        movie={movie}
                                        moveIcon={<MoveDownIcon />}
                                        disableMove={isPoolLocked}
                                        disableDelete={isPoolLocked}
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
                                        user={user}
                                        movie={movie}
                                        moveIcon={<MoveUpIcon />}
                                        disableMove={isPoolLocked || isPoolFull}
                                    />
                                </AnimatedListItem>
                            ))
                        ) : (
                            <p className="text-gray-500 col-span-full text-center">No movies in personal stash</p>
                        )}
                    </div>
                </CardContent>
            </Card>
        </div>
    );
}

function MovieItem({ user, movie, moveIcon, disableMove, disableDelete }: MovieItemProps) {
    const queryClient = useQueryClient();
    const [deleteModalIsOpen, toggleDeleteModal] = useToggle(false);

    const deleteMutation = useMutation({
        mutationFn: () => APIClient.users.deleteMovie(user.userID, movie.movieID),
        onSuccess: () => {
            toast.success(`Movie ${movie.title} deleted!`);
            void queryClient.invalidateQueries({ queryKey: UsersKeys.list() })
            void queryClient.invalidateQueries({ queryKey: MoviesKeys.listpool() })
        },
        onError: () => {
            toast.error(`Error deleting movie`);
        }
    })

    const moveMutation = useMutation({
        mutationFn: () => APIClient.users.moveMovie(user.userID, movie.movieID),
        onSuccess: () => {
            void queryClient.invalidateQueries({ queryKey: UsersKeys.list() })
            void queryClient.invalidateQueries({ queryKey: MoviesKeys.listpool() })
        },
        onError: () => {
            toast.error(`Error moving movie`);
        }
    })

    return (
        <>
            <DeletionDialog
                isOpen={deleteModalIsOpen}
                onClose={toggleDeleteModal}
                onConfirm={() => deleteMutation.mutate()}
                title="Delete Movie"
                description={`Are you sure you want to delete ${movie.title}? This action cannot be undone.`}
                confirmText="Delete"
                cancelText="Cancel"
            />

            <div className="flex items-center justify-between py-1">
                <div className="flex items-center gap-2 overflow-hidden">
                    <FilmIcon className="w-4 h-4 shrink-0" />
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
                            <LinkIcon />
                        </a>
                    </Button>
                    <Button
                        variant="ghost"
                        size="icon"
                        className="h-8 w-8"
                        onClick={() => moveMutation.mutate()}
                        disabled={disableMove}
                    >
                        {moveIcon}
                    </Button>
                    <Button
                        variant="ghost"
                        size="icon"
                        className="h-8 w-8 hover:bg-destructive"
                        onClick={toggleDeleteModal}
                        disabled={disableDelete}
                    >
                        <Trash2Icon />
                    </Button>
                </div>
            </div>
        </>
    );
}
