import { useMutation } from "@tanstack/react-query";
import { FilmIcon, LinkIcon, PencilIcon } from "lucide-react";

import { APIClient } from "@/api/APIClient";

import { EditMovieDialog } from "@/components/EditMovieDialog";
import { Button } from "@/components/ui/button";
import { toast } from "@/components/ui/toast";

import type { Movie } from "@/types/Response";

import { useToggle } from "@/hooks/hooks";


interface MovieItemProps {
  movie: Movie;
  watched?: boolean;
}

export function MovieItem({ movie, watched = false }: MovieItemProps) {
  const [editModalIsOpen, toggleEditModal] = useToggle(false);

  const editMutation = useMutation({
    mutationFn: (payload: { title: string; link: string; watchedAt?: string }) =>
      APIClient.users.updateMovie(
        movie.addedByID,
        movie.movieID,
        payload.title,
        payload.link,
        watched ? payload.watchedAt : undefined
      ),
    onSuccess: () => {
      toast.success(`Movie ${movie.title} updated!`);
      toggleEditModal();
    },
    onError: () => {
      toast.error("Error updating movie");
    },
  });

  return (
    <>
      <EditMovieDialog
        isOpen={editModalIsOpen}
        onClose={toggleEditModal}
        initialTitle={movie.title}
        initialLink={movie.link}
        initialWatchedAt={movie.watchedAt}
        allowWatchedAtEdit={watched}
        isSaving={editMutation.isPending}
        onSubmit={(payload) => editMutation.mutate(payload)}
      />

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
                Watched on: {new Date(movie.watchedAt!).toLocaleString()}
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
        <div className="flex items-center gap-1 shrink-0">
          <Button
            variant="ghost"
            size="icon"
            className="size-8"
            onClick={toggleEditModal}
          >
            <PencilIcon/>
          </Button>
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
      </div>
    </>
  );
}
