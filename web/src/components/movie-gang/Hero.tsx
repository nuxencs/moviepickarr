import { useMutation, useQuery } from "@tanstack/react-query";
import { EyeIcon, Loader2Icon, ShuffleIcon } from "lucide-react";
import { type CSSProperties, useEffect, useRef, useState } from "react";

import { APIClient } from "@/api/APIClient";
import {
  MoviesGetCurrentQueryOptions,
  MoviesGetPoolQueryOptions,
  SettingsGetNextPickerQueryOptions,
} from "@/api/queries";

import { Avatar, MetaChips } from "@/components/movie-gang/Bits";
import { backdropBg, backdropUrl, externalLinks, hueOf } from "@/components/movie-gang/lib";
import { Poster } from "@/components/movie-gang/Poster";
import { toast } from "@/components/ui/toast";

import type { Movie } from "@/types/Response";

/** Stagger index for the pick-reveal; each slot settles a touch after the last. */
const ri = (i: number) => ({ "--i": i }) as CSSProperties;

/**
 * Two-layer backdrop crossfade. Each new `revealId` adds a layer (its image was
 * already preloaded + decoded by the Hero before the id bumped, so it paints in
 * one frame), fades it in over the outgoing layer with a slow settle-scale, then
 * prunes the old one. Reduced-motion collapses the fade to an instant swap.
 */
function Backdrop({ bg, revealId }: { bg: string; revealId: number }) {
  const [layers, setLayers] = useState<{ id: number; bg: string }[]>(() => [{ id: revealId, bg }]);
  const prev = useRef(revealId);

  useEffect(() => {
    if (revealId === prev.current) return;
    prev.current = revealId;
    // Keep at most the outgoing layer plus the incoming one.
    setLayers((ls) => [...ls.slice(-1), { id: revealId, bg }]);
  }, [revealId, bg]);

  const settle = (id: number) =>
    setLayers((ls) => (ls.length > 1 && ls[ls.length - 1].id === id ? ls.slice(-1) : ls));

  return (
    <div className="hero__bg-stack" aria-hidden="true">
      {layers.map((l, i) => (
        <div
          key={l.id}
          className={`hero__bgimg${i > 0 ? " hero__bgimg--enter" : ""}`}
          style={{ backgroundImage: l.bg }}
          onAnimationEnd={i > 0 ? () => settle(l.id) : undefined}
        />
      ))}
    </div>
  );
}

/**
 * Full-bleed cinematic banner for the current pick (Movies tab only).
 * Absorbs the old NextPicker: it carries the Mark-Watched / Pick-Random
 * actions and the next-picker chip.
 */
export function Hero() {
  const { data: current, isLoading } = useQuery(MoviesGetCurrentQueryOptions());
  const { data: pooled } = useQuery(MoviesGetPoolQueryOptions());
  const { data: nextPicker } = useQuery(SettingsGetNextPickerQueryOptions());

  const pickMutation = useMutation({
    mutationFn: () => APIClient.movies.getRandom(),
    onSuccess: () => toast.success("Movie picked"),
    onError: () => toast.error("Failed to pick a random movie"),
  });

  const watchMutation = useMutation({
    mutationFn: () => APIClient.movies.markWatched(),
    onSuccess: () => toast.success("Marked as watched"),
    onError: () => toast.error("Failed to mark as watched"),
  });

  // The pick we actually display lags `current`: when the pick changes we
  // preload + decode its backdrop first, then commit the new pick AND bump
  // `revealId` together — so the backdrop crossfade and the staggered content
  // reveal land in the same frame, never flash blank, and the loading
  // placeholder never gets its own reveal cycle (revealId stays 0 until ready).
  const [shown, setShown] = useState<Movie | null>(null);
  const [revealId, setRevealId] = useState(0);

  useEffect(() => {
    if (isLoading) return;
    const next = current ?? null;
    let cancelled = false;
    const commit = () => {
      if (cancelled) return;
      setShown(next);
      setRevealId((n) => n + 1);
    };
    const url = next?.backdropPath ? backdropUrl(next.backdropPath) : null;
    if (url) {
      const img = new Image();
      img.src = url;
      img.decode().then(commit, commit);
    } else {
      commit();
    }
    return () => {
      cancelled = true;
    };
    // `current` keeps a stable reference across no-op refetches (TanStack structural
    // sharing), so this re-runs only when the pick actually changes or first settles.
  }, [isLoading, current]);

  const pick = shown;
  const hue = hueOf(pick?.title ?? "Movie Gang");
  const bg = pick?.backdropPath ? `url(${backdropUrl(pick.backdropPath)})` : backdropBg(hue);
  const canPick = !pick && (pooled?.length ?? 0) > 0;

  return (
    <section className="hero" data-ready={revealId > 0 ? "" : undefined}>
      <Backdrop bg={bg} revealId={revealId} />
      <div className="hero__inner">
        <div className="hero__poster" key={`p-${revealId}`} style={ri(0)}>
          <Poster
            title={pick?.title ?? "No pick yet"}
            hue={hue}
            posterPath={pick?.posterPath}
            showTitle={!pick?.posterPath}
          />
        </div>

        <div className="hero__body" key={`b-${revealId}`}>
          <div className="hero__eyebrow eyebrow" style={ri(1)}>
            {pick ? `Current pick · chosen by ${pick.addedByName}` : "No movie selected"}
          </div>

          <h2 className="hero__title" style={ri(2)}>
            {pick?.title ?? "Pick tonight's movie"}
          </h2>

          {/* Tagline + meta slots are always rendered (reserved height in CSS) so
              the banner never re-lays-out as the pick / its metadata changes. */}
          <p className="hero__tag" style={ri(3)}>
            {pick?.tagline
              ? `"${pick.tagline}"`
              : pick
                ? null
                : (pooled?.length ?? 0) > 0
                  ? "The pool is stocked. Spin for a random pick."
                  : "Add movies to the pool to get started."}
          </p>

          <div className="hero__meta" style={ri(4)}>
            {pick && <MetaChips movie={pick} links={externalLinks(pick)} />}
          </div>

          <div className="hero__actions" style={ri(5)}>
            {pick ? (
              <button
                type="button"
                className="btn btn--accent"
                onClick={() => watchMutation.mutate()}
                disabled={watchMutation.isPending}
              >
                {watchMutation.isPending ? <Loader2Icon className="animate-spin mg-spin" /> : <EyeIcon />}
                {watchMutation.isPending ? "Marking…" : "Mark as Watched"}
              </button>
            ) : (
              <button
                type="button"
                className="btn btn--accent"
                onClick={() => pickMutation.mutate()}
                disabled={!canPick || pickMutation.isPending}
              >
                {pickMutation.isPending ? <Loader2Icon className="animate-spin mg-spin" /> : <ShuffleIcon />}
                {pickMutation.isPending ? "Picking…" : "Pick Random Movie"}
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
