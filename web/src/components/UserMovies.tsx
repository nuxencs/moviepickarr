import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { FilmIcon, LinkIcon, MoveDownIcon, MoveUpIcon, SearchIcon, Trash2Icon } from 'lucide-react';
import { ReactNode, useMemo, useState } from 'react';

import { APIClient } from "@/api/APIClient";
import { SettingsGetPoolLockQueryOptions } from "@/api/queries";
import { MoviesKeys, UsersKeys } from "@/api/query_keys";

import { AddMovie } from '@/components/AddMovie';
import { AnimatedList, AnimatedListItem } from '@/components/ui/animated-list';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { DeletionDialog } from "@/components/ui/deletion-dialog";
import { Input } from '@/components/ui/input';
import { toast } from "@/components/ui/toast";

import { useToggle } from "@/hooks/hooks";
import { Movie, User } from "@/types/Response";

interface UserMoviesProps {
    user: User;
}

interface MovieItemProps {
    user: User;
    movie: Movie;
    moveIcon: ReactNode;
    disableMove?: boolean;
    disableDelete?: boolean;
}

export function UserMovies({ user }: UserMoviesProps) {
    const { data: isPoolLocked } = useQuery(SettingsGetPoolLockQueryOptions())
    const [stashSearchTerm, setStashSearchTerm] = useState("");

    const isPoolFull = Object.keys(user.currentPool).length >= 3;
    const userPool = useMemo(() => {
        return Object.values(user.currentPool).sort((a, b) => a.title.toLowerCase() > b.title.toLowerCase() ? 1 : -1)
    }, [user.currentPool]);
    const userStash = useMemo(() => {
        return Object.values(user.stash).sort((a, b) => a.title.toLowerCase() > b.title.toLowerCase() ? 1 : -1)
    }, [user.stash]);
    const filteredUserStash = useMemo(() =>
        userStash.filter((movie) =>
            movie.title.toLowerCase().includes(stashSearchTerm.toLowerCase())
        ), [userStash, stashSearchTerm]);

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
                    <AnimatedList className="space-y-1">
                        {userPool.map((movie) => (
                            <AnimatedListItem key={movie.movieID} id={movie.movieID}>
                                <MovieItem
                                    user={user}
                                    movie={movie}
                                    moveIcon={<MoveDownIcon />}
                                    disableMove={isPoolLocked}
                                    disableDelete={isPoolLocked}
                                />
                            </AnimatedListItem>
                        ))}
                        {Array.from({ length: Math.max(0, 3 - userPool.length) }).map((_, i) => (
                            <div
                                key={`placeholder-${i}`}
                                className="flex items-center justify-between py-0.5 px-2.5 bg-gray-50 dark:bg-gray-800/50 rounded border-2 border-dashed border-gray-300 dark:border-gray-600"
                            >
                                <div className="flex items-center gap-2 overflow-hidden">
                                    <FilmIcon className="size-4 shrink-0 text-gray-400" />
                                    <span className="truncate text-gray-500 dark:text-gray-400">
                                        Empty slot
                                    </span>
                                </div>
                                <div className="size-8 flex-shrink-0"></div>
                            </div>
                        ))}
                    </AnimatedList>
                </CardContent>
            </Card>

            <Card>
                <CardHeader className="pb-2">
                    <CardTitle className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
                        <span className="shrink-0">Stash ({filteredUserStash.length}{stashSearchTerm ? `/${userStash.length}` : ''})</span>
                        <div className="flex items-center gap-4">
                            <SearchIcon className="size-4 shrink-0 text-gray-400" />
                            <Input
                                placeholder="Search stash..."
                                value={stashSearchTerm}
                                onChange={(e) => setStashSearchTerm(e.target.value)}
                                className="w-full text-sm"
                            />
                        </div>
                    </CardTitle>
                </CardHeader>
                <CardContent className="pr-2">
                    {filteredUserStash.length > 0 ? (
                        <AnimatedList className="space-y-1 pr-4 max-h-64 rounded-md overflow-auto">
                            {filteredUserStash.map((movie) => (
                                <AnimatedListItem key={movie.movieID} id={movie.movieID} className="truncate">
                                    <MovieItem
                                        user={user}
                                        movie={movie}
                                        moveIcon={<MoveUpIcon />}
                                        disableMove={isPoolLocked || isPoolFull}
                                    />
                                </AnimatedListItem>
                            ))}
                        </AnimatedList>
                    ) : (
                        <p className="text-gray-500 col-span-full text-center">
                          No movies in personal stash
                        </p>
                    )}
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
                <div className="flex items-center gap-2 overflow-auto">
                    <FilmIcon className="size-4 shrink-0" />
                    <span className="truncate">{movie.title}</span>
                </div>
                <div className="flex items-center gap-1 shrink-0">
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
                    <Button
                        variant="ghost"
                        size="icon"
                        className="size-8"
                        onClick={() => moveMutation.mutate()}
                        disabled={disableMove}
                    >
                        {moveIcon}
                    </Button>
                    <Button
                        variant="ghost"
                        size="icon"
                        className="size-8 hover:bg-destructive"
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
