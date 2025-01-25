import React, {useState} from 'react';
import {Button} from '@/components/ui/button';
import {Film, Link, MoveLeft, MoveRight, Trash2} from 'lucide-react';
import {AnimatedListItem} from '@/components/ui/animated-list';
import AddMovie from './AddMovie';
import {Card, CardContent, CardHeader, CardTitle} from '@/components/ui/card';
import {ConfirmDialog} from "@/components/ui/confirm-dialog";
import {Movie} from "@/types/Response.ts";

interface UserMoviesProps {
    pooledMovies: Record<string, Movie>;
    stashedMovies: Record<string, Movie>;
    userID: string;
    onMovieDelete: (movieID: string) => void;
    onMovieAdd: (movie: Movie) => void;
    onMovieMove: (movieID: string) => void;
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

const UserMovies: React.FC<UserMoviesProps> = ({
                                                   pooledMovies,
                                                   stashedMovies,
                                                   userID,
                                                   onMovieDelete,
                                                   onMovieAdd,
                                                   onMovieMove,
                                               }) => {
    const [movieToDelete, setMovieToDelete] = useState<Movie | null>(null);
    const isPoolFull = Object.keys(pooledMovies).length >= 3;

    const handleDeleteConfirm = () => {
        if (movieToDelete) {
            onMovieDelete(movieToDelete.movieID);
            setMovieToDelete(null);
        }
    };

    return (
        <div className="space-y-4">
            <AddMovie
                userID={userID}
                onMovieAdded={onMovieAdd}
            />

            <Card>
                <CardHeader className="pb-2">
                    <CardTitle className="text-sm">
                        Pool ({Object.keys(pooledMovies).length}/3)
                    </CardTitle>
                </CardHeader>
                <CardContent>
                    <div className="space-y-1">
                        {Object.keys(pooledMovies).length > 0 ? (
                            Object.values(pooledMovies).map((movie) => (
                                <AnimatedListItem key={movie.movieID} id={movie.movieID}>
                                    <MovieItem
                                        movie={movie}
                                        onDelete={setMovieToDelete}
                                        onMove={() => onMovieMove(movie.movieID)}
                                        moveIcon={<MoveRight className="w-4 h-4"/>}
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
                        Stash ({Object.keys(stashedMovies).length})
                    </CardTitle>
                </CardHeader>
                <CardContent>
                    <div className="space-y-1">
                        {Object.keys(stashedMovies).length > 0 ? (
                            Object.values(stashedMovies).map((movie) => (
                                <AnimatedListItem key={movie.movieID} id={movie.movieID}>
                                    <MovieItem
                                        movie={movie}
                                        onDelete={setMovieToDelete}
                                        onMove={() => onMovieMove(movie.movieID)}
                                        moveIcon={<MoveLeft className="w-4 h-4"/>}
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
                onConfirm={handleDeleteConfirm}
                title="Delete Movie"
                description={`Are you sure you want to delete ${movieToDelete?.title}? This action cannot be undone.`}
                confirmText="Delete"
                cancelText="Cancel"
            />
        </div>
    );
};

export default UserMovies;
