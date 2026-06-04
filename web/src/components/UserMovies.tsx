import { useMutation, useQuery } from "@tanstack/react-query";
import { EllipsisIcon, FilmIcon, LinkIcon, MoveDownIcon, MoveUpIcon, PencilIcon, SearchIcon, Trash2Icon } from 'lucide-react';
import { ReactNode, useMemo, useState } from 'react';

import { APIClient } from "@/api/APIClient";
import { SettingsGetPoolLockQueryOptions } from "@/api/queries";

import { EditMovieDialog } from "@/components/EditMovieDialog";
import { SearchMovie } from "@/components/SearchMovie.tsx";
import { AnimatedList, AnimatedListItem } from '@/components/ui/animated-list';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { DeletionDialog } from "@/components/ui/deletion-dialog";
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "@/components/ui/dropdown-menu";
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
      <SearchMovie userID={user.userID}/>

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
                  moveIcon={<MoveDownIcon/>}
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
                  <FilmIcon className="size-4 shrink-0 text-gray-400"/>
                  <span className="truncate text-gray-500 dark:text-gray-400">
                    Empty slot
                  </span>
                </div>
                <div className="w-[5.75rem] flex-shrink-0"></div>
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
              <SearchIcon className="size-4 shrink-0 text-gray-400"/>
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
                    moveIcon={<MoveUpIcon/>}
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
  const [deleteModalIsOpen, toggleDeleteModal] = useToggle(false);
  const [editModalIsOpen, toggleEditModal] = useToggle(false);

  const deleteMutation = useMutation({
    mutationFn: () => APIClient.users.deleteMovie(user.userID, movie.movieID),
    onSuccess: () => {
      toast.success(`Movie ${movie.title} deleted!`);
    },
    onError: () => {
      toast.error(`Error deleting movie`);
    }
  })

  const moveMutation = useMutation({
    mutationFn: () => APIClient.users.moveMovie(user.userID, movie.movieID),
    onError: () => {
      toast.error(`Error moving movie`);
    }
  })

  const editMutation = useMutation({
    mutationFn: (payload: { title: string; link: string }) =>
      APIClient.users.updateMovie(user.userID, movie.movieID, payload.title, payload.link),
    onSuccess: () => {
      toast.success(`Movie ${movie.title} updated!`);
      toggleEditModal();
    },
    onError: () => {
      toast.error("Error updating movie");
    }
  })

  return (
    <>
      <EditMovieDialog
        isOpen={editModalIsOpen}
        onClose={toggleEditModal}
        initialTitle={movie.title}
        initialLink={movie.link}
        isSaving={editMutation.isPending}
        onSubmit={(payload) =>
          editMutation.mutate({ title: payload.title, link: payload.link })
        }
      />

      <DeletionDialog
        isOpen={deleteModalIsOpen}
        onClose={toggleDeleteModal}
        onConfirm={() => deleteMutation.mutate()}
        title="Delete Movie"
        description={`Are you sure you want to delete ${movie.title}? This action cannot be undone.`}
        confirmText="Delete"
        cancelText="Cancel"
      />

      <div className="flex items-center justify-between py-1 gap-2">
        <div className="min-w-0 flex items-center gap-2 overflow-hidden">
          <FilmIcon className="size-4 shrink-0"/>
          <span className="truncate">{movie.title}</span>
        </div>
        <div className="flex w-[5.75rem] items-center justify-end gap-0.5 shrink-0">
          <Button
            variant="ghost"
            size="icon"
            className="size-7"
            asChild
          >
            <a href={movie.link} target="_blank" rel="noopener noreferrer">
              <LinkIcon/>
            </a>
          </Button>
          <Button
            variant="ghost"
            size="icon"
            className="size-7"
            onClick={() => moveMutation.mutate()}
            disabled={disableMove}
          >
            {moveIcon}
          </Button>

          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button
                variant="ghost"
                size="icon"
                className="size-7"
                aria-label="More actions"
              >
                <EllipsisIcon/>
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end">
              <DropdownMenuItem onSelect={toggleEditModal}>
                <PencilIcon/>
                Edit
              </DropdownMenuItem>
              <DropdownMenuItem
                onSelect={toggleDeleteModal}
                disabled={disableDelete}
                className="bg-destructive/10 text-destructive font-semibold data-[highlighted]:bg-destructive/20 data-[highlighted]:text-destructive"
              >
                <Trash2Icon/>
                Delete
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        </div>
      </div>
    </>
  );
}
