import { useMutation, useQuery } from "@tanstack/react-query";
import { EyeIcon, ShuffleIcon } from "lucide-react";

import { APIClient } from "@/api/APIClient";
import {
  MoviesGetCurrentQueryOptions,
  MoviesGetPoolQueryOptions,
  SettingsGetNextPickerQueryOptions,
} from "@/api/queries";

import { Avatar, MetaChips } from "@/components/movie-gang/Bits";
import { backdropBg, backdropUrl, hueOf } from "@/components/movie-gang/lib";
import { Poster } from "@/components/movie-gang/Poster";
import { toast } from "@/components/ui/toast";

/**
 * Full-bleed cinematic banner for the current pick (Movies tab only).
 * Absorbs the old NextPicker: it carries the Mark-Watched / Pick-Random
 * actions and the next-picker chip.
 */
export function Hero() {
  const { data: current } = useQuery(MoviesGetCurrentQueryOptions());
  const { data: pooled } = useQuery(MoviesGetPoolQueryOptions());
  const { data: nextPicker } = useQuery(SettingsGetNextPickerQueryOptions());

  const pickMutation = useMutation({
    mutationFn: () => APIClient.movies.getRandom(),
    onSuccess: () => toast.success("Movie picked!"),
    onError: () => toast.error("Failed to pick a random movie"),
  });

  const watchMutation = useMutation({
    mutationFn: () => APIClient.movies.markWatched(),
    onSuccess: () => toast.success("Marked as watched"),
    onError: () => toast.error("Failed to mark as watched"),
  });

  const hue = hueOf(current?.title ?? "Movie Gang");
  const bg = current?.backdropPath
    ? `url(${backdropUrl(current.backdropPath)})`
    : backdropBg(hue);

  const canPick = !current && (pooled?.length ?? 0) > 0;

  return (
    <section className="hero">
      <div className="hero__bg" style={{ backgroundImage: bg }} />
      <div className="hero__inner">
        <div className="hero__poster">
          <Poster
            title={current?.title ?? "No pick yet"}
            hue={hue}
            posterPath={current?.posterPath}
            voteAverage={current?.voteAverage}
            showTitle={!current?.posterPath}
          />
        </div>

        <div className="hero__body">
          <div className="hero__eyebrow eyebrow">
            <span className="live" />
            {current
              ? `Current pick · chosen by ${current.addedByName}`
              : "No movie selected"}
          </div>

          <h2 className="hero__title">{current?.title ?? "Pick tonight's movie"}</h2>

          {current?.tagline ? (
            <p className="hero__tag">"{current.tagline}"</p>
          ) : (
            !current && (
              <p className="hero__tag">
                {(pooled?.length ?? 0) > 0
                  ? "The pool is stocked — spin for a random pick."
                  : "Add movies to the pool to get started."}
              </p>
            )
          )}

          {current && (
            <div className="hero__meta">
              <MetaChips movie={current} />
            </div>
          )}

          <div className="hero__actions">
            {current ? (
              <button
                type="button"
                className="btn btn--accent"
                onClick={() => watchMutation.mutate()}
                disabled={watchMutation.isPending}
              >
                <EyeIcon />
                Mark as Watched
              </button>
            ) : (
              <button
                type="button"
                className="btn btn--accent"
                onClick={() => pickMutation.mutate()}
                disabled={!canPick || pickMutation.isPending}
              >
                <ShuffleIcon />
                Pick Random Movie
              </button>
            )}

            {nextPicker?.name && (
              <div className="hero__nextpick">
                <Avatar name={nextPicker.name} size={30} />
                <div>
                  <div className="lab">Next picker</div>
                  <div className="nm">{nextPicker.name}</div>
                </div>
              </div>
            )}
          </div>
        </div>
      </div>
    </section>
  );
}
