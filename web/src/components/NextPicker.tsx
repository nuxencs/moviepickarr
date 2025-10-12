import { APIClient } from "@/api/APIClient";
import {
  MoviesGetCurrentQueryOptions,
  MoviesGetPoolQueryOptions,
  SettingsGetNextPickerQueryOptions,
} from "@/api/queries";

import { MovieItem } from "@/components/MovieItem";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { toast } from "@/components/ui/toast";

import { useMutation, useQuery } from "@tanstack/react-query";
import { EyeIcon, FilmIcon, ShuffleIcon, UserIcon } from "lucide-react";

export function NextPicker() {
  const { data: nextPicker, isLoading: nextPickerLoading } = useQuery(SettingsGetNextPickerQueryOptions());
  const { data: currentMovie } = useQuery(MoviesGetCurrentQueryOptions());
  const { data: pooledMovies } = useQuery(MoviesGetPoolQueryOptions());

  const pickMutation = useMutation({
    mutationFn: () => APIClient.movies.getRandom(),
    onSuccess: () => {
      toast.success("Movie picked successfully!");
    },
    onError: () => {
      toast.error("Failed to pick a random movie");
    },
  });

  const watchMutation = useMutation({
    mutationFn: () => APIClient.movies.markWatched(),
    onSuccess: () => {
      toast.success("Movie marked as watched!");
    },
    onError: () => {
      toast.error("Failed to mark movie as watched");
    },
  });

  if (nextPickerLoading) {
    return (
      <Card>
        <CardContent className="pt-6">
          <p className="text-center text-gray-500">Loading...</p>
        </CardContent>
      </Card>
    );
  }

  if (!nextPicker || !nextPicker.name) {
    return (
      <Card>
        <CardContent className="pt-6">
          <p className="text-center text-gray-500">No users available</p>
        </CardContent>
      </Card>
    );
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
          <div className="flex items-center gap-3">
            <div className="size-8 rounded-full bg-gray-100 dark:bg-gray-800 flex items-center justify-center">
              <UserIcon className="size-4 text-gray-600 dark:text-gray-400"/>
            </div>
            <div className="flex flex-col">
              <span className="font-medium">{nextPicker.name}</span>
              <span className="text-sm text-gray-500 font-normal">Next picker</span>
            </div>
          </div>
          <div className="flex gap-2 flex-col sm:flex-row">
            <Button
              onClick={() => pickMutation.mutate()}
              disabled={
                pickMutation.isPending ||
                pooledMovies?.length === 0 ||
                currentMovie !== null
              }
              className="w-full sm:w-auto"
            >
              <ShuffleIcon className="mr-2 size-4"/>
              Pick Random Movie
            </Button>
            <Button
              variant="default"
              onClick={() => watchMutation.mutate()}
              disabled={watchMutation.isPending || !currentMovie}
              className="w-full sm:w-auto"
            >
              <EyeIcon className="mr-2 size-4"/>
              Mark as Watched
            </Button>
          </div>
        </CardTitle>
      </CardHeader>
      <CardContent>
        <div className="space-y-4">
          {currentMovie ? (
            <MovieItem movie={currentMovie}/>
          ) : (
            // p-2.5 (10px) instead of p-3 (12px) to compensate for the dashed border with a width of 2px
            <div className="flex items-center justify-between p-2.5 bg-gray-50 dark:bg-gray-800/50 rounded border-2 border-dashed border-gray-300 dark:border-gray-600">
              <div className="flex items-center gap-2 overflow-hidden">
                <FilmIcon className="size-4 shrink-0 text-gray-400"/>
                <span className="truncate text-gray-500 dark:text-gray-400">
                  No movie selected
                </span>
              </div>
              <div className="size-8 flex-shrink-0"></div>
            </div>
          )}
        </div>
      </CardContent>
    </Card>
  );
}
