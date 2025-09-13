import { FilmIcon, LinkIcon } from "lucide-react";

import { Button } from "@/components/ui/button";
import type { Movie } from "@/types/Response";

interface MovieItemProps {
    movie: Movie;
    watched?: boolean;
}

export function MovieItem({ movie, watched = false }: MovieItemProps) {
    return (
        <div className="flex items-center truncate justify-between p-3 bg-gray-100 dark:bg-gray-800 rounded">
            <div className="flex items-center truncate gap-2">
                <FilmIcon className="size-4 shrink-0"/>
                {watched ? (
                    <div className="flex flex-col truncate">
                        <span className="truncate">
                            {movie.title}
                            <span className="pl-2 text-gray-400">
                                ({movie.addedByName})
                            </span>
                        </span>
                        <span className="truncate text-sm text-gray-500">
                            Watched on: {new Date(movie.watchedAt!).toLocaleDateString()}
                        </span>
                    </div>
                ) : (
                    <span className="truncate">
                        {movie.title}
                        <span className="pl-2 text-gray-400">
                            ({movie.addedByName})
                        </span>
                    </span>
                )}
            </div>
            <Button
                variant="ghost"
                size="icon"
                className="size-8 flex-shrink-0"
                asChild
            >
                <a href={movie.link} target="_blank" rel="noopener noreferrer">
                    <LinkIcon/>
                </a>
            </Button>
        </div>
    );
}