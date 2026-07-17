import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { EyeIcon, Loader2Icon, ShuffleIcon } from "lucide-react";
import { type CSSProperties, useCallback, useEffect, useRef, useState } from "react";

import { APIClient } from "@/api/APIClient";
import {
  MoviesGetCurrentQueryOptions,
  MoviesGetPoolQueryOptions,
  SettingsGetNextUpQueryOptions,
} from "@/api/queries";
import { MoviesKeys, DrawKeys, SettingsKeys } from "@/api/query_keys";

import { Avatar, MetaChips } from "@/components/moviepickarr/Bits";
import { DrawReel } from "@/components/moviepickarr/DrawReel";
import {
  type ActiveSpin,
  buildLiveSpin,
  buildResumeSpin,
  clearActiveSpin,
  drawAwaitingReveal,
  setActiveSpin,
} from "@/components/moviepickarr/drawSpin";
import { backdropBg, backdropUrl, externalLinks, hueOf } from "@/components/moviepickarr/lib";
import { Poster } from "@/components/moviepickarr/Poster";
import { toast } from "@/components/ui/toast-api";

import type { Movie } from "@/types/Response";

/** Stagger index for the draw-reveal; each slot settles a touch after the last. */
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

// Draws already turned into a reel this page session. Module-level (not component
// state) so it survives Hero remounts — leaving and returning to the Movies tab
// must NOT replay a spin that already ran — while still resetting on a full page
// reload, so a genuine reload can resume an in-flight spin.
const handledDraws = new Set<string>();

/**
 * Full-bleed cinematic banner for the current draw (Movies tab only).
 * Absorbs the old next-up panel: it carries the Mark-Watched / Draw-Random
 * actions and the next-up chip.
 */
export function Hero() {
  const queryClient = useQueryClient();
  const { data: current, isLoading } = useQuery(MoviesGetCurrentQueryOptions());
  const { data: pooled } = useQuery(MoviesGetPoolQueryOptions());
  const { data: nextUp } = useQuery(SettingsGetNextUpQueryOptions());

  // Reactive read of the cross-client spin signal, set via setQueryData by the
  // SSE handler (movie:drawn) and the draw mutation below.
  const { data: activeSpin } = useQuery<ActiveSpin | null>({
    queryKey: DrawKeys.active(),
    queryFn: () => null,
    staleTime: Infinity,
    gcTime: Infinity,
    refetchOnWindowFocus: false,
  });

  // Cross-client close signal: the drawnAt of a draw that was revealed (the
  // drawer confirmed, or a countdown filled), set by the useSSE movie:revealed
  // handler. When it matches the in-flight spin, this client closes its reel too.
  const { data: revealSignal } = useQuery<string | null>({
    queryKey: DrawKeys.revealed(),
    queryFn: () => null,
    staleTime: Infinity,
    gcTime: Infinity,
    refetchOnWindowFocus: false,
  });

  // Held from the moment the action button is clicked until the hero has actually
  // moved on. The POST resolves (and isPending drops) before the transition lands —
  // for `marking`, before the current-draw refetch; for `drawing`, before the reel
  // takes over (set in an effect, a frame later) or the no-reel refetch commits — so
  // without these the button flashes back to its resting label in that gap instead of
  // settling straight on the next state.
  const [marking, setMarking] = useState(false);
  const [drawing, setDrawing] = useState(false);

  const drawMutation = useMutation({
    mutationFn: () => APIClient.movies.getRandom(),
    // Hold the button busy from the click; released only once the new draw is
    // revealed (see the `drawing` effect below).
    onMutate: () => setDrawing(true),
    onSuccess: (movie) => {
      // No toast here — the reel itself is the draw feedback; a "Movie drawn"
      // toast popping while the reel is still spinning just competes with it.
      // Fallback if the clicker's own SSE event drops: start the reel from the
      // response (which carries its own candidates) and pull in the winner +
      // rotated state without the SSE-driven invalidation. setActiveSpin dedups
      // against the SSE event by drawnAt.
      const spin = buildLiveSpin(movie);
      setActiveSpin(queryClient, spin);
      void queryClient.invalidateQueries({ queryKey: MoviesKeys.current() });
      void queryClient.invalidateQueries({ queryKey: SettingsKeys.nextUp() });
      // Hold the pool refresh until the reel lands (see onLand) so the pool grid
      // doesn't drop the winner mid-spin and spoil the result. No reel → now.
      if (!spin) void queryClient.invalidateQueries({ queryKey: MoviesKeys.listpool() });
    },
    onError: () => {
      setDrawing(false);
      toast.error("Failed to draw a random movie");
    },
  });

  const watchMutation = useMutation({
    mutationFn: () => APIClient.movies.markWatched(),
    // Hold the button busy from the click; released only once the watched draw
    // actually leaves the hero (see the `marking` effect below).
    onMutate: () => setMarking(true),
    onSuccess: () => {
      toast.success("Marked as watched");
      // Clear the current draw ourselves instead of waiting on the SSE
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
  // land). `committed` is the last draw we revealed, so an unrelated pool refetch
  // re-running the commit effect can't replay the reveal. (`handledDraws`, which
  // stops a re-render or a tab-switch remount from restarting a spin, is
  // module-level so it outlives this component's mount.)
  const [spinning, setSpinning] = useState(false);
  const [spinDescriptor, setSpinDescriptor] = useState<ActiveSpin | null>(null);
  const committed = useRef<Movie | null | undefined>(undefined);
  // Mirrors of the latest values, so the stable `reveal` callback (held by
  // DrawReel across renders and by the SSE-signal effect) reads current state.
  // `revealedDraw` is the drawnAt already handed off, so reveal() runs once per
  // draw no matter which trigger fires first (OK press, countdown, or SSE).
  const spinDescriptorRef = useRef<ActiveSpin | null>(null);
  spinDescriptorRef.current = spinDescriptor;
  const currentRef = useRef<Movie | null | undefined>(current);
  currentRef.current = current;
  const revealedDrawRef = useRef<string | null>(null);

  // Close the reel and hand off to the hero reveal — once per draw. A "local"
  // trigger (the drawer's OK, or any client's countdown self-heal) also tells the
  // server, which broadcasts movie:revealed so the other clients close in step; a
  // "remote" trigger IS that broadcast, so it doesn't echo back. The winner's
  // backdrop (preloaded during the spin) is decoded while the reel still covers
  // the hero, then the reel drops + the reveal commits in ONE batched render — so
  // no placeholder frame leaks through between the reel closing and the reveal.
  const reveal = useCallback(
    (source: "local" | "remote") => {
      const desc = spinDescriptorRef.current;
      if (!desc || revealedDrawRef.current === desc.drawnAt) return;
      revealedDrawRef.current = desc.drawnAt;
      if (source === "local") void APIClient.movies.reveal().catch(() => {});
      const next = currentRef.current ?? null;
      const finish = () => {
        committed.current = next;
        setShown(next);
        setRevealId((n) => n + 1);
        setSpinning(false);
        clearActiveSpin(queryClient);
        void queryClient.invalidateQueries({ queryKey: MoviesKeys.listpool() });
      };
      const url = next?.backdropPath ? backdropUrl(next.backdropPath) : null;
      if (url) {
        const img = new Image();
        img.src = url;
        img.decode().then(finish, finish);
      } else {
        finish();
      }
    },
    [queryClient],
  );
  // Stable zero-arg confirm handler for the reel (a fresh arrow each render would
  // reset DrawReel's countdown timer on every Hero re-render).
  const confirmLocal = useCallback(() => reveal("local"), [reveal]);

  // A movie:revealed signal for the in-flight draw (the drawer confirmed, or a
  // countdown filled on some client) closes this client's reel too.
  useEffect(() => {
    if (revealSignal && spinDescriptor && revealSignal === spinDescriptor.drawnAt) {
      reveal("remote");
    }
  }, [revealSignal, spinDescriptor, reveal]);

  // Start the reel when a spin signal arrives (SSE draw / clicker fallback).
  // Declared before the commit effect so its handledDraws write lands first.
  useEffect(() => {
    if (!activeSpin || handledDraws.has(activeSpin.drawnAt)) return;
    handledDraws.add(activeSpin.drawnAt);
    setSpinDescriptor(activeSpin);
    setSpinning(true);
  }, [activeSpin]);

  // The draw we actually display lags `current`: when the draw changes we preload
  // + decode its backdrop first, then commit the new draw AND bump `revealId`
  // together — so the backdrop crossfade and the staggered content reveal land in
  // the same frame, never flash blank, and the loading placeholder never gets its
  // own reveal cycle (revealId stays 0 until ready). While the reel spins the
  // commit is held back, so the reveal hands off the instant it lands.
  useEffect(() => {
    if (isLoading) return;
    if (spinning) return; // the reel owns the transition; commit waits for the land

    const next = current ?? null;

    // Reload mid-spin: resume the reel from the time that's left, before
    // committing, so the winner never flashes ahead of it. Decided once per draw.
    // The window check needs only `current`; building the reel needs the pool, so
    // hold the commit until the pool has loaded.
    if (next?.drawnAt && !handledDraws.has(next.drawnAt)) {
      if (drawAwaitingReveal(next)) {
        if (pooled === undefined) return;
        handledDraws.add(next.drawnAt);
        const resume = buildResumeSpin(next, pooled);
        if (resume) {
          setSpinDescriptor(resume);
          setSpinning(true);
          return;
        }
      } else {
        handledDraws.add(next.drawnAt);
      }
    }

    // Only (re)reveal when the draw IDENTITY changes. Comparing object reference
    // (relying on TanStack structural sharing) is too fragile: the current-draw
    // endpoint stamps a fresh `serverNow` on every request, so a no-op refetch —
    // the SSE resync on tab refocus, or an enrichment update — returns a
    // structurally-different object for the SAME draw. That churns the reference
    // and would replay the whole reveal (backdrop crossfade + staggered content)
    // on every tab switch. Key on drawnAt + movieID, which are stable for a given
    // draw, instead. `committed.current === undefined` means nothing has committed
    // yet — distinct from a committed empty state (null).
    const sameDraw =
      committed.current !== undefined &&
      (current?.drawnAt ?? null) === (committed.current?.drawnAt ?? null) &&
      (current?.movieID ?? null) === (committed.current?.movieID ?? null);
    if (sameDraw) {
      // Same draw, possibly churned metadata: refresh the shown object so any
      // late-arriving fields land, but DON'T bump revealId — the hero must stay
      // static across tab switches and never re-animate for an unchanged draw.
      committed.current = current;
      setShown(next);
      return;
    }

    let cancelled = false;
    const commit = () => {
      if (cancelled) return;
      // Claim the draw only when the reveal actually lands — NOT synchronously
      // before the async backdrop decode. onLand invalidates the pool, so this
      // effect re-runs (pooled dep) and its cleanup cancels the in-flight decode;
      // if `committed` had already claimed the draw, the guard above would skip
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
    // The sameDraw guard above stops this from replaying the reveal on no-op
    // refetches (serverNow churn, enrichment, resync), so it re-animates only when
    // the draw actually changes; the pool/spin deps just re-run the resume check.
  }, [isLoading, current, spinning, pooled]);

  // Release the marking busy-state once the watched draw has left the hero (shown
  // cleared by the commit effect above), so the action button goes Marking… → Draw
  // random movie with no flash back to "Mark as watched" in between.
  useEffect(() => {
    if (marking && !shown) setMarking(false);
  }, [marking, shown]);

  // Mirror of the above for the draw flow: release the drawing busy-state once the
  // new draw is revealed (shown set), so the button goes Drawing… → Mark as watched
  // with no flash back to "Draw random movie" in the pre-reel frame or the no-reel gap.
  useEffect(() => {
    if (drawing && shown) setDrawing(false);
  }, [drawing, shown]);

  const draw = shown;
  // False until the first draw (or confirmed-empty) has committed after its
  // backdrop decoded. While loading we render a quiet banner shell — no
  // placeholder copy ("Draw next movie") flashing before the real draw.
  const ready = revealId > 0;
  const hue = hueOf(draw?.title ?? "moviepickarr");
  const bg = draw?.backdropPath ? `url(${backdropUrl(draw.backdropPath)})` : backdropBg(hue);
  const canDraw = !draw && (pooled?.length ?? 0) > 0;

  return (
    <section className="hero" data-ready={revealId > 0 ? "" : undefined}>
      <Backdrop bg={bg} revealId={revealId} />
      <div className="hero__inner">
        <div className="hero__poster" key={`p-${revealId}`} style={ri(0)}>
          <Poster
            title={draw?.title ?? "No draw yet"}
            hue={hue}
            posterPath={draw?.posterPath}
            showTitle={ready && !draw?.posterPath}
          />
        </div>

        <div className="hero__body" key={`b-${revealId}`}>
          <div className="hero__eyebrow eyebrow" style={ri(1)}>
            {!ready ? (
              ""
            ) : draw ? (
              <>
                Current draw · chosen by <strong className="hero__by">{draw.addedByName}</strong>
              </>
            ) : (
              "No movie selected"
            )}
          </div>

          <h2 className="hero__title" style={ri(2)}>
            {!ready ? "" : (draw?.title ?? "Draw next movie")}
          </h2>

          {/* Tagline + meta slots are always rendered (reserved height in CSS) so
              the banner never re-lays-out as the draw / its metadata changes. */}
          <p className="hero__tag" style={ri(3)}>
            {!ready
              ? null
              : draw?.tagline
                ? `"${draw.tagline}"`
                : draw
                  ? null
                  : (pooled?.length ?? 0) > 0
                    ? "The pool is stocked. Spin for a random draw."
                    : "Add movies to the pool to get started."}
          </p>

          <div className="hero__meta" style={ri(4)}>
            {ready && draw && <MetaChips movie={draw} links={externalLinks(draw)} />}
          </div>

          <div className="hero__actions" style={ri(5)}>
            {ready &&
              (marking || drawing ? (
                // Held from the click until the transition settles (the watched draw
                // leaves the hero, or the new draw is revealed), so the button never
                // regresses to its resting label mid-transition. See `marking`/`drawing`.
                <button type="button" className="btn btn--accent" disabled aria-busy="true">
                  <Loader2Icon className="animate-spin mg-spin" />
                  {marking ? "Marking…" : "Drawing…"}
                </button>
              ) : draw ? (
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
                  onClick={() => drawMutation.mutate()}
                  disabled={!canDraw}
                >
                  <ShuffleIcon />
                  Draw random movie
                </button>
              ))}

            {ready && nextUp?.name && (
              <div className="hero__nextup">
                <Avatar name={nextUp.name} size={30} />
                <div>
                  <div className="lab">Next up</div>
                  <div className="nm">{nextUp.name}</div>
                </div>
              </div>
            )}
          </div>
        </div>
      </div>

      {spinning && spinDescriptor && (
        <DrawReel
          key={spinDescriptor.drawnAt}
          spin={spinDescriptor}
          onConfirm={confirmLocal}
        />
      )}
    </section>
  );
}
