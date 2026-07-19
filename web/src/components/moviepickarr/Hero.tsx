import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { EyeIcon, Loader2Icon, ShuffleIcon } from "lucide-react";
import { type CSSProperties, useEffect, useRef, useState, useSyncExternalStore } from "react";

import { APIClient, ApiError } from "@/api/APIClient";
import {
  MoviesGetCurrentQueryOptions,
  MoviesGetPoolQueryOptions,
  SettingsGetNextUpQueryOptions,
} from "@/api/queries";
import { MoviesKeys, SettingsKeys } from "@/api/query_keys";

import { Avatar, MetaChips } from "@/components/moviepickarr/Bits";
import { drawAwaitingReveal } from "@/components/moviepickarr/drawMachine";
import { DrawReel } from "@/components/moviepickarr/DrawReel";
import { drawStore, resolveDrawEnv } from "@/components/moviepickarr/drawStore";
import { backdropBg, backdropUrl, externalLinks, hueOf } from "@/components/moviepickarr/lib";
import { Poster } from "@/components/moviepickarr/Poster";
import { drawLockedTip, revealLockedTip, useTurnGate, watchLockedTip } from "@/components/moviepickarr/turnGate";
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

// Stable senders for the reel. Module scope, so a Hero re-render never resets
// DrawReel's internal state via a changed prop identity; the draw machine
// behind them owns dedup and reveal-once, so duplicate sends are silent.
const reportScrollDone = () => drawStore.send({ type: "SCROLL_DONE" });
const confirmDraw = () => drawStore.send({ type: "CONFIRM", source: "local" });

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
      // Clear the current draw ourselves instead of waiting on the SSE
      // movie:watched round-trip — keeps the hero transition snappy and self-
      // sufficient if the stream lags. (The SSE event still re-invalidates; the
      // duplicate refetch is a harmless no-op once current is already null.)
      void queryClient.invalidateQueries({ queryKey: MoviesKeys.current() });
    },
    onError: (err) => {
      setMarking(false);
      onTurnError(err, "Failed to mark as watched");
    },
  });

  const [shown, setShown] = useState<Movie | null>(null);
  const [revealId, setRevealId] = useState(0);
  // The last draw committed to the hero, so an unrelated refetch re-running
  // the commit effect can't replay the reveal (see the sameDraw guard below).
  const committed = useRef<Movie | null | undefined>(undefined);

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
    committed.current = current;
    setShown(current ?? null);
    setRevealId((n) => n + 1);
  }

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
    // the draw actually changes; the pool/seen deps just re-run the resume check.
  }, [isLoading, current, spinning, pooled, drawState.seen]);

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
                Current draw · added by <strong className="hero__by">{draw.addedByName}</strong>
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
                <div>
                  <div className="lab">Next up</div>
                  <div className="nm">{nextUp.name}</div>
                </div>
              </div>
            )}
          </div>
        </div>
      </div>

      {spinning && drawState.spin && (
        <DrawReel
          key={drawState.spin.drawnAt}
          spin={drawState.spin}
          canReveal={gate.canAct}
          revealTip={revealLockedTip(gate)}
          onScrollDone={reportScrollDone}
          onConfirm={confirmDraw}
        />
      )}
    </section>
  );
}
