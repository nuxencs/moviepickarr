import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { EyeIcon, Loader2Icon, ShuffleIcon } from "lucide-react";
import {
  type CSSProperties,
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
  useSyncExternalStore,
} from "react";

import { APIClient, ApiError } from "@/api/APIClient";
import { setCachedDrawInProgress } from "@/api/poolStateCache";
import {
  MoviesGetCurrentQueryOptions,
  MoviesGetPoolQueryOptions,
  SettingsGetNextUpQueryOptions,
} from "@/api/queries";
import { MoviesKeys, SettingsKeys, UsersKeys } from "@/api/query_keys";

import { Avatar, MetaChips } from "@/components/moviepickarr/Bits";
import { drawAwaitingReveal } from "@/components/moviepickarr/drawMachine";
import { DrawReel } from "@/components/moviepickarr/DrawReel";
import { drawStore, resolveDrawEnv } from "@/components/moviepickarr/drawStore";
import { backdropBg, backdropUrl, externalLinks, hueOf } from "@/components/moviepickarr/lib";
import { possessive } from "@/components/moviepickarr/possessive";
import { Poster } from "@/components/moviepickarr/Poster";
import { drawLockedTip, revealLockedTip, useTurnGate, watchLockedTip } from "@/components/moviepickarr/turnGate";
import { toast } from "@/components/ui/toast-api";

import type { MovieDetail } from "@/types/Response";

/** Stagger index for the draw-reveal; each slot settles a touch after the last. */
const ri = (i: number) => ({ "--i": i }) as CSSProperties;

/**
 * Two-layer backdrop crossfade. Each painted-art revision adds a decoded layer,
 * fades it in over the outgoing layer with a slow settle-scale, then prunes the
 * old one. Reduced-motion collapses the fade to an instant swap.
 */
function Backdrop({ bg, revision }: { bg: string; revision: number }) {
  const [layers, setLayers] = useState<{ id: number; bg: string }[]>(() => [{ id: revision, bg }]);
  const prev = useRef(revision);

  useLayoutEffect(() => {
    if (revision === prev.current) return;
    prev.current = revision;
    // Keep at most the outgoing layer plus the incoming one.
    setLayers((ls) => [...ls.slice(-1), { id: revision, bg }]);
  }, [revision, bg]);

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

// Stable senders for the reel. Module scope, so a Hero re-render never resets
// DrawReel's internal state via a changed prop identity; the draw machine
// behind them owns dedup and reveal-once, so duplicate sends are silent.
const reportScrollDone = () => drawStore.send({ type: "SCROLL_DONE" });
const confirmDraw = () => drawStore.send({ type: "CONFIRM", source: "local" });

const drawIdentity = (movie: MovieDetail | null): string =>
  movie ? `${movie.movieID}:${movie.drawnAt ?? ""}` : "none";

function artworkDescriptor(movie: MovieDetail | null) {
  const identity = drawIdentity(movie);
  const url = backdropUrl(movie?.backdropPath);
  const fallback = backdropBg(hueOf(movie?.title ?? "moviepickarr"));
  const source = url ? `${identity}:${url}` : `${identity}:fallback:${movie?.title ?? "moviepickarr"}`;
  return { identity, source, url, fallback };
}

interface HeroArtwork {
  revision: number;
  identity: string;
  source: string;
  bg: string;
}

interface ArtworkTarget {
  source: string;
  settled: boolean;
  pending?: Promise<void>;
}

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
  // The board-level turn gate: whether this viewer (admin, or the next-up
  // member) may run the draw → reveal → watch cycle. Drives the disabled +
  // tooltip treatment on the action buttons and the reel's reveal control.
  const gate = useTurnGate();

  // The backstop for a lost race: the turn passed (or the roster changed)
  // between render and click, so the backend answers 403 not_next_up. Refresh
  // the actor + next-up so the board re-gates, and name why the action bounced
  // instead of the generic failure toast.
  const onTurnError = (err: unknown, fallback: string): void => {
    if (err instanceof ApiError && err.status === 403) {
      void queryClient.invalidateQueries({ queryKey: SettingsKeys.nextUp() });
      void queryClient.invalidateQueries({ queryKey: MoviesKeys.current() });
      toast.error("It's not your turn right now");
      return;
    }
    toast.error(fallback);
  };

  // The draw machine drives the reel: phase + spin descriptor come from the
  // store singleton, fed by useSSE (movie:drawn / movie:revealed) and the draw
  // mutation below. The machine owns dedup, resume, and reveal-once.
  const drawState = useSyncExternalStore(drawStore.subscribe, drawStore.getState);
  const spinning = drawState.phase !== "idle";

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
      setCachedDrawInProgress(queryClient, true);
      // No toast here — the reel itself is the draw feedback; a "Movie drawn"
      // toast popping while the reel is still spinning just competes with it.
      // Fallback if the clicker's own SSE event drops: feed the machine from
      // the response (which carries its own candidates). The machine dedups
      // against the SSE event by drawnAt and owns the pool-refresh timing
      // (held until the reel lands; immediate when no reel will play).
      drawStore.send({ type: "DRAWN", movie });
      void queryClient.invalidateQueries({ queryKey: MoviesKeys.current() });
      void queryClient.invalidateQueries({ queryKey: SettingsKeys.nextUp() });
    },
    onError: (err) => {
      setDrawing(false);
      onTurnError(err, "Failed to draw a random movie");
    },
  });

  const watchMutation = useMutation({
    mutationFn: () => APIClient.movies.markWatched(),
    // Hold the button busy from the click; released only once the watched draw
    // actually leaves the hero (see the `marking` effect below).
    onMutate: () => setMarking(true),
    onSuccess: () => {
      toast.success("Marked as watched");
      setCachedDrawInProgress(queryClient, false);
      // Clear the current draw ourselves instead of waiting on the SSE
      // movie:watched round-trip — keeps the hero transition snappy and self-
      // sufficient if the stream lags. (The SSE event still re-invalidates; the
      // duplicate refetch is a harmless no-op once current is already null.)
      void queryClient.invalidateQueries({ queryKey: MoviesKeys.current() });
      void queryClient.invalidateQueries({ queryKey: MoviesKeys.listpool() });
      void queryClient.invalidateQueries({ queryKey: UsersKeys.list() });
    },
    onError: (err) => {
      setMarking(false);
      onTurnError(err, "Failed to mark as watched");
    },
  });

  const [shown, setShown] = useState<MovieDetail | null>(null);
  const [revealId, setRevealId] = useState(0);
  const [artwork, setArtwork] = useState<HeroArtwork>(() => ({
    revision: 0,
    identity: "none",
    source: "initial",
    bg: backdropBg(hueOf("moviepickarr")),
  }));
  // Keeps one decode alive across Strict Mode's effect replay. Object identity
  // also prevents an older promise from painting after the source changes.
  const artworkTarget = useRef<ArtworkTarget | null>(null);
  // The last draw committed to the hero, so an unrelated refetch re-running
  // the commit effect can't replay the reveal (see the sameDraw guard below).
  const committed = useRef<MovieDetail | null | undefined>(undefined);

  // Reveal handoff: the machine bumps commitSeq once the winner's backdrop has
  // decoded, in the SAME store update that drops the reel (phase → idle).
  // Committing during render (React's adjust-state-on-change pattern) keeps
  // the reel unmount and the hero reveal in one paint: no placeholder frame
  // leaks through. Initialized to the current seq so a Hero remount (tab
  // switch) never replays a reveal that already committed. The assignments are
  // idempotent, so a re-invoked render pass is harmless.
  const [seenCommitSeq, setSeenCommitSeq] = useState(drawState.commitSeq);
  if (drawState.commitSeq !== seenCommitSeq) {
    setSeenCommitSeq(drawState.commitSeq);
    const next = current ?? null;
    const nextArtwork = artworkDescriptor(next);
    const decodedBackdrop = drawState.decodedBackdrop;
    const reuseDecodedBackdrop =
      nextArtwork.url !== null &&
      decodedBackdrop !== null &&
      decodedBackdrop.movieID === next?.movieID &&
      decodedBackdrop.drawnAt === next?.drawnAt &&
      decodedBackdrop.backdropPath === next?.backdropPath;
    committed.current = current;
    setShown(next);
    setRevealId((n) => n + 1);
    // The machine already proved the draw payload's backdrop paintable. Reuse
    // it when the current query still agrees; a changed path falls through to
    // the normal fallback + decode path below.
    artworkTarget.current =
      reuseDecodedBackdrop || !nextArtwork.url
        ? { source: nextArtwork.source, settled: true }
        : null;
    setArtwork((previous) => ({
      revision: previous.revision + 1,
      identity: nextArtwork.identity,
      source:
        reuseDecodedBackdrop || !nextArtwork.url
          ? nextArtwork.source
          : `${nextArtwork.identity}:fallback`,
      bg: reuseDecodedBackdrop ? `url(${nextArtwork.url})` : nextArtwork.fallback,
    }));
  }

  // Commit known content without waiting on the network. Artwork has its own
  // decoded handoff below, so a slow or failed image cannot leave the title and
  // actions blank. While the reel spins, it still owns the transition.
  useEffect(() => {
    if (isLoading) return;
    if (spinning) return; // the reel owns the transition; commit waits for the land

    const next = current ?? null;

    // Reload mid-spin: hand the pending draw to the machine before committing,
    // so the winner never flashes ahead of the reel. The machine dedups draws
    // it already handled (drawState.seen). The reveal-pending check needs only
    // `current`; building the reel needs the pool, so hold the commit until
    // the pool has loaded. An already-revealed draw is still sent: the
    // machine marks it handled and the commit below shows the result directly.
    if (next?.drawnAt && !drawState.seen.includes(next.drawnAt)) {
      if (drawAwaitingReveal(next, resolveDrawEnv())) {
        if (pooled === undefined) return;
        drawStore.send({ type: "RESUME", current: next, pool: pooled });
        return; // the store subscription re-renders with the spin (or as seen)
      }
      drawStore.send({ type: "RESUME", current: next, pool: pooled ?? [] });
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

    committed.current = current;
    setShown(next);
    setRevealId((n) => n + 1);
    // The sameDraw guard above stops this from replaying the reveal on no-op
    // refetches (serverNow churn, enrichment, resync), so it re-animates only when
    // the draw actually changes; the pool/seen deps just re-run the resume check.
  }, [isLoading, current, spinning, pooled, drawState.seen]);

  const desiredArtwork = artworkDescriptor(shown);

  // Swap a changed draw to its own procedural art before the browser paints.
  // Same-draw path changes intentionally keep the current decoded layer.
  useLayoutEffect(() => {
    if (isLoading || spinning) return;
    setArtwork((previous) => {
      const keepCurrent =
        previous.identity === desiredArtwork.identity &&
        (desiredArtwork.url !== null || previous.source === desiredArtwork.source);
      return keepCurrent
        ? previous
        : {
            revision: previous.revision + 1,
            identity: desiredArtwork.identity,
            source: desiredArtwork.url
              ? `${desiredArtwork.identity}:fallback`
              : desiredArtwork.source,
            bg: desiredArtwork.fallback,
          };
    });
  }, [
    isLoading,
    spinning,
    desiredArtwork.identity,
    desiredArtwork.source,
    desiredArtwork.url,
    desiredArtwork.fallback,
  ]);

  // A remote source does not depend on fallback colour. Keeping this null for
  // remote art lets metadata-only title changes leave an in-flight decode alone.
  const desiredArtworkFallback = desiredArtwork.url ? null : desiredArtwork.fallback;

  // Decode controls only the painted art. A new draw gets its own procedural
  // fallback immediately so old artwork is not shown under new content. A path
  // update for the same draw keeps the current layer until its replacement is
  // paintable. The source key is stable across metadata-only refetches.
  useEffect(() => {
    if (isLoading || spinning) return;

    if (!desiredArtwork.url) {
      if (desiredArtworkFallback === null) return;
      if (
        artworkTarget.current?.source === desiredArtwork.source &&
        artworkTarget.current.settled
      ) {
        return;
      }
      artworkTarget.current = { source: desiredArtwork.source, settled: true };
      setArtwork((previous) =>
        previous.source === desiredArtwork.source
          ? previous
          : {
              revision: previous.revision + 1,
              identity: desiredArtwork.identity,
              source: desiredArtwork.source,
              bg: desiredArtworkFallback,
            },
      );
      return;
    }

    let target = artworkTarget.current;
    if (target?.source === desiredArtwork.source && target.settled) return;

    if (target?.source !== desiredArtwork.source || !target.pending) {
      const image = new Image();
      image.src = desiredArtwork.url;
      target = {
        source: desiredArtwork.source,
        settled: false,
        pending: image.decode(),
      };
      artworkTarget.current = target;
    }

    let cancelled = false;
    const commit = () => {
      if (cancelled || artworkTarget.current !== target) return;
      target.settled = true;
      target.pending = undefined;
      setArtwork((previous) =>
        previous.source === desiredArtwork.source
          ? previous
          : {
              revision: previous.revision + 1,
              identity: desiredArtwork.identity,
              source: desiredArtwork.source,
              bg: `url(${desiredArtwork.url})`,
            },
      );
    };
    const reject = () => {
      if (cancelled || artworkTarget.current !== target) return;
      target.settled = true;
      target.pending = undefined;
    };
    const pending = target.pending;
    if (!pending) return;
    pending.then(commit, reject);

    return () => {
      cancelled = true;
    };
  }, [
    isLoading,
    spinning,
    desiredArtwork.identity,
    desiredArtwork.source,
    desiredArtwork.url,
    desiredArtworkFallback,
  ]);

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
  // False until the first draw (or confirmed-empty) has committed. While
  // loading we render a quiet banner shell — no
  // placeholder copy ("Draw next movie") flashing before the real draw.
  const ready = revealId > 0;
  const hue = hueOf(draw?.title ?? "moviepickarr");
  const canDraw = !draw && (pooled?.length ?? 0) > 0;

  return (
    <section className="hero" data-ready={revealId > 0 ? "" : undefined}>
      <Backdrop bg={artwork.bg} revision={artwork.revision} />
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
                {/* The same way from a film to whoever stashed it as the modal's
                    attribution (#238). A push, not a replace: the entry it
                    leaves is the Movies page's own, so Back comes back to the
                    draw. The modal replaces because the entry it leaves is the
                    modal's, and nothing here is holding one. An archived adder
                    keeps the credit but has no active board to link to. */}
                Current draw · added by{" "}
                {draw.addedByArchived ? (
                  <span className="hero__by">{draw.addedByName}</span>
                ) : (
                  <Link
                    to="/users"
                    search={{ member: draw.addedByID }}
                    className="hero__by"
                    title={`See ${possessive(draw.addedByName)} board`}
                  >
                    {draw.addedByName}
                  </Link>
                )}
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
                // Stays visible for spectators, disabled with a tooltip naming
                // the next-up member, so the turn reads instead of the control
                // vanishing (the backend not_next_up is the backstop).
                <button
                  type="button"
                  className="btn btn--accent"
                  onClick={() => watchMutation.mutate()}
                  disabled={gate.locked}
                  title={gate.locked ? watchLockedTip(gate) : undefined}
                >
                  <EyeIcon />
                  Mark as watched
                </button>
              ) : (
                <button
                  type="button"
                  className="btn btn--accent"
                  onClick={() => drawMutation.mutate()}
                  disabled={!canDraw || gate.locked}
                  title={gate.locked ? drawLockedTip(gate) : undefined}
                >
                  <ShuffleIcon />
                  Draw random movie
                </button>
              ))}

            {ready && nextUp?.name && (
              <div className="hero__nextup">
                <Avatar name={nextUp.name} size={30} />
                <div className="nm">{gate.isSelf ? "Your turn" : `${possessive(gate.nextUpName)} turn`}</div>
              </div>
            )}
          </div>
        </div>
      </div>

      {spinning && drawState.spin && (
        <DrawReel
          key={drawState.spin.drawnAt}
          spin={drawState.spin}
          phase={drawState.phase}
          canReveal={gate.canAct}
          revealTip={revealLockedTip(gate)}
          onScrollDone={reportScrollDone}
          onConfirm={confirmDraw}
        />
      )}
    </section>
  );
}
