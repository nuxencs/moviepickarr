import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { EyeIcon, Loader2Icon, ShuffleIcon } from "lucide-react";
import { type CSSProperties, useEffect, useRef, useState } from "react";

import { APIClient } from "@/api/APIClient";
import {
  MoviesGetCurrentQueryOptions,
  MoviesGetPoolQueryOptions,
  SettingsGetNextPickerQueryOptions,
} from "@/api/queries";
import { MoviesKeys, PickKeys, SettingsKeys } from "@/api/query_keys";

import { Avatar, MetaChips } from "@/components/movie-gang/Bits";
import { backdropBg, backdropUrl, externalLinks, hueOf } from "@/components/movie-gang/lib";
import { PickReel } from "@/components/movie-gang/PickReel";
import {
  type ActiveSpin,
  buildLiveSpin,
  buildResumeSpin,
  clearActiveSpin,
  isWithinSpinWindow,
  setActiveSpin,
} from "@/components/movie-gang/pickSpin";
import { Poster } from "@/components/movie-gang/Poster";
import { toast } from "@/components/ui/toast-api";

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

// Picks already turned into a reel this page session. Module-level (not component
// state) so it survives Hero remounts — leaving and returning to the Movies tab
// must NOT replay a spin that already ran — while still resetting on a full page
// reload, so a genuine reload can resume an in-flight spin.
const handledPicks = new Set<string>();

/**
 * Full-bleed cinematic banner for the current pick (Movies tab only).
 * Absorbs the old NextPicker: it carries the Mark-Watched / Pick-Random
 * actions and the next-picker chip.
 */
export function Hero() {
  const queryClient = useQueryClient();
  const { data: current, isLoading } = useQuery(MoviesGetCurrentQueryOptions());
  const { data: pooled } = useQuery(MoviesGetPoolQueryOptions());
  const { data: nextPicker } = useQuery(SettingsGetNextPickerQueryOptions());

  // Reactive read of the cross-client spin signal, set via setQueryData by the
  // SSE handler (movie:picked) and the pick mutation below.
  const { data: activeSpin } = useQuery<ActiveSpin | null>({
    queryKey: PickKeys.active(),
    queryFn: () => null,
    staleTime: Infinity,
    gcTime: Infinity,
    refetchOnWindowFocus: false,
  });

  // True from the moment "Mark as watched" is clicked until the watched pick has
  // actually left the hero. The POST resolves (and isPending drops) before the
  // current-pick refetch lands, so without this the action button flashes back to
  // "Mark as watched" in that gap instead of settling straight on "Pick random movie".
  const [marking, setMarking] = useState(false);

  const pickMutation = useMutation({
    mutationFn: () => APIClient.movies.getRandom(),
    onSuccess: (movie) => {
      toast.success("Movie picked");
      // Fallback if the clicker's own SSE event drops: start the reel from the
      // response and pull in the winner + rotated state without the SSE-driven
      // invalidation. setActiveSpin dedups against the SSE event by pickedAt.
      const poolSnapshot = queryClient.getQueryData<Movie[]>(MoviesKeys.listpool()) ?? [];
      const spin = buildLiveSpin(movie, poolSnapshot);
      setActiveSpin(queryClient, spin);
      void queryClient.invalidateQueries({ queryKey: MoviesKeys.current() });
      void queryClient.invalidateQueries({ queryKey: SettingsKeys.nextPicker() });
      // Hold the pool refresh until the reel lands (see onLand) so the pool grid
      // doesn't drop the winner mid-spin and spoil the result. No reel → now.
      if (!spin) void queryClient.invalidateQueries({ queryKey: MoviesKeys.listpool() });
    },
    onError: () => toast.error("Failed to pick a random movie"),
  });

  const watchMutation = useMutation({
    mutationFn: () => APIClient.movies.markWatched(),
    // Hold the button busy from the click; released only once the watched pick
    // actually leaves the hero (see the `marking` effect below).
    onMutate: () => setMarking(true),
    onSuccess: () => {
      toast.success("Marked as watched");
      // Clear the current pick ourselves instead of waiting on the SSE
      // movie:watched round-trip — keeps the hero transition snappy and self-
      // sufficient if the stream lags. (The SSE event still re-invalidates; the
      // duplicate refetch is a harmless no-op once current is already null.)
      void queryClient.invalidateQueries({ queryKey: MoviesKeys.current() });
    },
    onError: () => {
      setMarking(false);
      toast.error("Failed to mark as watched");
    },
  });

  const [shown, setShown] = useState<Movie | null>(null);
  const [revealId, setRevealId] = useState(0);
  // Reel state: `spinning` mounts the takeover overlay; `spinDescriptor` is the
  // spin it renders (held locally so it survives clearing the shared signal on
  // land). `committed` is the last pick we revealed, so an unrelated pool refetch
  // re-running the commit effect can't replay the reveal. (`handledPicks`, which
  // stops a re-render or a tab-switch remount from restarting a spin, is
  // module-level so it outlives this component's mount.)
  const [spinning, setSpinning] = useState(false);
  const [spinDescriptor, setSpinDescriptor] = useState<ActiveSpin | null>(null);
  const committed = useRef<Movie | null | undefined>(undefined);

  // Start the reel when a spin signal arrives (SSE pick / clicker fallback).
  // Declared before the commit effect so its handledPicks write lands first.
  useEffect(() => {
    if (!activeSpin || handledPicks.has(activeSpin.pickedAt)) return;
    handledPicks.add(activeSpin.pickedAt);
    setSpinDescriptor(activeSpin);
    setSpinning(true);
  }, [activeSpin]);

  // The pick we actually display lags `current`: when the pick changes we preload
  // + decode its backdrop first, then commit the new pick AND bump `revealId`
  // together — so the backdrop crossfade and the staggered content reveal land in
  // the same frame, never flash blank, and the loading placeholder never gets its
  // own reveal cycle (revealId stays 0 until ready). While the reel spins the
  // commit is held back, so the reveal hands off the instant it lands.
  useEffect(() => {
    if (isLoading) return;
    if (spinning) return; // the reel owns the transition; commit waits for the land

    const next = current ?? null;

    // Reload mid-spin: resume the reel from the time that's left, before
    // committing, so the winner never flashes ahead of it. Decided once per pick.
    // The window check needs only `current`; building the reel needs the pool, so
    // hold the commit until the pool has loaded.
    if (next?.pickedAt && !handledPicks.has(next.pickedAt)) {
      if (isWithinSpinWindow(next)) {
        if (pooled === undefined) return;
        handledPicks.add(next.pickedAt);
        const resume = buildResumeSpin(next, pooled);
        if (resume) {
          setSpinDescriptor(resume);
          setSpinning(true);
          return;
        }
      } else {
        handledPicks.add(next.pickedAt);
      }
    }

    // Only (re)reveal when the pick itself changed — not when an unrelated pool
    // refetch re-ran this effect (pooled is a dep for the resume decision above).
    if (current === committed.current) return;

    let cancelled = false;
    const commit = () => {
      if (cancelled) return;
      // Claim the pick only when the reveal actually lands — NOT synchronously
      // before the async backdrop decode. onLand invalidates the pool, so this
      // effect re-runs (pooled dep) and its cleanup cancels the in-flight decode;
      // if `committed` had already claimed the pick, the guard above would skip
      // the re-commit and the hero would stay stuck on the previous frame.
      committed.current = current;
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
    // sharing), so this re-runs only when the pick changes, the pool loads, or the
    // reel lands.
  }, [isLoading, current, spinning, pooled]);

  // Release the marking busy-state once the watched pick has left the hero (shown
  // cleared by the commit effect above), so the action button goes Marking… → Pick
  // random movie with no flash back to "Mark as watched" in between.
  useEffect(() => {
    if (marking && !shown) setMarking(false);
  }, [marking, shown]);

  const pick = shown;
  // False until the first pick (or confirmed-empty) has committed after its
  // backdrop decoded. While loading we render a quiet banner shell — no
  // placeholder copy ("Pick next movie") flashing before the real pick.
  const ready = revealId > 0;
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
            showTitle={ready && !pick?.posterPath}
          />
        </div>

        <div className="hero__body" key={`b-${revealId}`}>
          <div className="hero__eyebrow eyebrow" style={ri(1)}>
            {!ready ? (
              ""
            ) : pick ? (
              <>
                Current pick · chosen by <strong className="hero__by">{pick.addedByName}</strong>
              </>
            ) : (
              "No movie selected"
            )}
          </div>

          <h2 className="hero__title" style={ri(2)}>
            {!ready ? "" : (pick?.title ?? "Pick next movie")}
          </h2>

          {/* Tagline + meta slots are always rendered (reserved height in CSS) so
              the banner never re-lays-out as the pick / its metadata changes. */}
          <p className="hero__tag" style={ri(3)}>
            {!ready
              ? null
              : pick?.tagline
                ? `"${pick.tagline}"`
                : pick
                  ? null
                  : (pooled?.length ?? 0) > 0
                    ? "The pool is stocked. Spin for a random pick."
                    : "Add movies to the pool to get started."}
          </p>

          <div className="hero__meta" style={ri(4)}>
            {ready && pick && <MetaChips movie={pick} links={externalLinks(pick)} />}
          </div>

          <div className="hero__actions" style={ri(5)}>
            {ready &&
              (marking ? (
                // Held from click until the watched pick clears (see `marking`), so
                // the button never regresses to "Mark as watched" mid-transition.
                <button type="button" className="btn btn--accent" disabled aria-busy="true">
                  <Loader2Icon className="animate-spin mg-spin" />
                  Marking…
                </button>
              ) : pick ? (
                <button
                  type="button"
                  className="btn btn--accent"
                  onClick={() => watchMutation.mutate()}
                >
                  <EyeIcon />
                  Mark as watched
                </button>
              ) : (
                <button
                  type="button"
                  className="btn btn--accent"
                  onClick={() => pickMutation.mutate()}
                  disabled={!canPick || pickMutation.isPending}
                >
                  {pickMutation.isPending ? <Loader2Icon className="animate-spin mg-spin" /> : <ShuffleIcon />}
                  {pickMutation.isPending ? "Picking…" : "Pick random movie"}
                </button>
              ))}

            {ready && nextPicker?.name && (
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

      {spinning && spinDescriptor && (
        <PickReel
          key={spinDescriptor.pickedAt}
          spin={spinDescriptor}
          onLand={() => {
            setSpinning(false);
            clearActiveSpin(queryClient);
            // Reveal the post-pick pool (the winner leaves the grid) in lockstep
            // with the hero reveal, not the instant Pick was clicked.
            void queryClient.invalidateQueries({ queryKey: MoviesKeys.listpool() });
          }}
        />
      )}
    </section>
  );
}
