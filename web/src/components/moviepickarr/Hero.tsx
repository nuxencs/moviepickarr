import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { AsteriskIcon, EyeIcon, Loader2Icon, RefreshCwIcon, ShuffleIcon, SkipForwardIcon, XIcon } from "lucide-react";
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
  MoviesGetWildcardQueryOptions,
  SettingsGetNextUpQueryOptions,
  SettingsGetPoolStateQueryOptions,
} from "@/api/queries";
import { MoviesKeys, SettingsKeys, UsersKeys } from "@/api/query_keys";

import { Avatar, MetaChips } from "@/components/moviepickarr/Bits";
import { drawAwaitingReveal } from "@/components/moviepickarr/drawMachine";
import { DrawReel } from "@/components/moviepickarr/DrawReel";
import { drawStore, resolveDrawEnv } from "@/components/moviepickarr/drawStore";
import { backdropBg, backdropUrl, externalLinks, hueOf } from "@/components/moviepickarr/lib";
import { MovieModal } from "@/components/moviepickarr/MovieModal";
import { possessive } from "@/components/moviepickarr/possessive";
import { Poster } from "@/components/moviepickarr/Poster";
import { drawLockedTip, guestWildcardTip, revealLockedTip, useTurnGate, watchLockedTip } from "@/components/moviepickarr/turnGate";
import { WildcardModal } from "@/components/moviepickarr/WildcardModal";
import { DeletionDialog } from "@/components/ui/deletion-dialog";
import { toast } from "@/components/ui/toast-api";

import type { MovieDetail } from "@/types/Response";

import { useMovieModal } from "@/hooks/useMovieModalHistory";

/** Stagger index for the draw-reveal. */
const ri = (i: number) => ({ "--i": i }) as CSSProperties;

/** Two-layer backdrop crossfade: each revision fades in over the outgoing layer, then prunes it. */
function Backdrop({ bg, revision }: { bg: string; revision: number }) {
  const [layers, setLayers] = useState<{ id: number; bg: string }[]>(() => [{ id: revision, bg }]);
  const prev = useRef(revision);

  useLayoutEffect(() => {
    if (revision === prev.current) return;
    prev.current = revision;
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

// Module scope, so a Hero re-render never resets DrawReel via a changed prop identity.
const reportScrollDone = () => drawStore.send({ type: "SCROLL_DONE" });
const confirmDraw = () => drawStore.send({ type: "CONFIRM", source: "local" });

/** Names why a Turn skip bounced. */
function skipErrorMessage(err: unknown): string {
  if (!(err instanceof ApiError)) return "Failed to skip the turn";
  switch (err.code) {
    case "next_up_changed":
      return "The turn already moved. Check who is next up and try again.";
    case "draw_not_revealed":
      return "Wait for the draw to be revealed before you skip the turn.";
    case "conflict":
      return "There is no other member to pass the turn to.";
    default:
      return "Failed to skip the turn";
  }
}

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

/** Full-bleed banner for the current draw, with the turn actions and the next-up chip. */
export function Hero() {
  const queryClient = useQueryClient();
  const { data: current, isLoading } = useQuery(MoviesGetCurrentQueryOptions());
  const { data: pooled } = useQuery(MoviesGetPoolQueryOptions());
  const wildcardQuery = useQuery(MoviesGetWildcardQueryOptions());
  const { data: wildcard } = wildcardQuery;
  const wildcardStateKnown = wildcard !== undefined && !wildcardQuery.isError;
  const { data: nextUp } = useQuery(SettingsGetNextUpQueryOptions());
  // The drawer owns an unrevealed draw's turn, so the Turn skip waits for the reveal (409 is the backstop).
  const { data: poolState } = useQuery(SettingsGetPoolStateQueryOptions());
  const drawUnrevealed = poolState === undefined || poolState.drawInProgress;
  const gate = useTurnGate();

  // Lost race: the turn passed between render and click (403 not_next_up). Refresh so the board re-gates.
  const onTurnError = (err: unknown, fallback: string): void => {
    if (err instanceof ApiError && err.status === 403) {
      void queryClient.invalidateQueries({ queryKey: SettingsKeys.nextUp() });
      void queryClient.invalidateQueries({ queryKey: MoviesKeys.current() });
      toast.error(err.message || "You cannot use this action right now");
      return;
    }
    toast.error(fallback);
  };

  // Fed by useSSE and the draw mutation; the machine owns dedup, resume, and reveal-once.
  const drawState = useSyncExternalStore(drawStore.subscribe, drawStore.getState);
  const spinning = drawState.phase !== "idle";

  // Held from click until the hero moves on: isPending drops before the transition
  // lands, so without these the button flashes back to its resting label.
  const [marking, setMarking] = useState(false);
  const [drawing, setDrawing] = useState(false);
  const [wildcardPickerHostID, setWildcardPickerHostID] = useState<number | null>(null);
  const [wildcardCancelID, setWildcardCancelID] = useState<number | null>(null);
  // The holder the admin saw when opening the skip confirm, sent as the stale guard.
  const [skipHolder, setSkipHolder] = useState<{ id: number; name: string } | null>(null);
  const heldDrawModal = useMovieModal();

  useEffect(() => {
    if (
      wildcardPickerHostID !== null
      && (current?.movieID !== wildcardPickerHostID || wildcard != null)
    ) {
      setWildcardPickerHostID(null);
    }
  }, [current?.movieID, wildcard, wildcardPickerHostID]);

  useEffect(() => {
    if (wildcardCancelID !== null && wildcardStateKnown && wildcard?.id !== wildcardCancelID) {
      setWildcardCancelID(null);
    }
  }, [wildcard?.id, wildcardCancelID, wildcardStateKnown]);

  const drawMutation = useMutation({
    mutationFn: () => APIClient.movies.getRandom(),
    onMutate: () => setDrawing(true),
    onSuccess: (movie) => {
      setCachedDrawInProgress(queryClient, true);
      // No toast: the reel is the feedback. Feeding the machine here covers a dropped
      // SSE event; it dedups against the SSE event by drawnAt.
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
    onMutate: () => setMarking(true),
    onSuccess: () => {
      toast.success("Marked as watched");
      setCachedDrawInProgress(queryClient, false);
      // Do not wait on the SSE movie:watched round-trip, so a lagging stream cannot stall the hero.
      void queryClient.invalidateQueries({ queryKey: MoviesKeys.current() });
      void queryClient.invalidateQueries({ queryKey: MoviesKeys.listpool() });
      void queryClient.invalidateQueries({ queryKey: UsersKeys.list() });
    },
    onError: (err) => {
      setMarking(false);
      onTurnError(err, "Failed to mark as watched");
    },
  });

  const watchWildcardMutation = useMutation({
    mutationFn: (wildcardID: number) => APIClient.movies.watchWildcard(wildcardID),
    onSuccess: () => {
      toast.success("Wildcard marked as watched");
      void queryClient.invalidateQueries({ queryKey: MoviesKeys.wildcard() });
      void queryClient.invalidateQueries({ queryKey: MoviesKeys.listwatched() });
      void queryClient.invalidateQueries({ queryKey: MoviesKeys.details() });
      void queryClient.invalidateQueries({ queryKey: UsersKeys.list() });
    },
    onError: () => {
      void queryClient.invalidateQueries({ queryKey: MoviesKeys.wildcard() });
      toast.error("Failed to mark the wildcard as watched");
    },
  });

  const cancelWildcardMutation = useMutation({
    mutationFn: (wildcardID: number) => APIClient.movies.cancelWildcard(wildcardID),
    onSuccess: () => {
      setWildcardCancelID(null);
      toast.success("Wildcard canceled");
      void queryClient.invalidateQueries({ queryKey: MoviesKeys.wildcard() });
      void queryClient.invalidateQueries({ queryKey: MoviesKeys.listpool() });
      void queryClient.invalidateQueries({ queryKey: UsersKeys.list() });
    },
    onError: () => {
      void queryClient.invalidateQueries({ queryKey: MoviesKeys.wildcard() });
      toast.error("Failed to cancel the wildcard");
    },
  });

  const skipMutation = useMutation({
    mutationFn: (holderID: number) => APIClient.settings.skipNextUp(holderID),
    onSuccess: (next) => {
      setSkipHolder(null);
      queryClient.setQueryData(SettingsKeys.nextUp(), next);
      toast.success(`Turn skipped. It's ${possessive(next.name)} turn.`);
    },
    onError: (err) => {
      setSkipHolder(null);
      void queryClient.invalidateQueries({ queryKey: SettingsKeys.nextUp() });
      void queryClient.invalidateQueries({ queryKey: MoviesKeys.current() });
      toast.error(skipErrorMessage(err));
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
  // Keeps one decode alive across Strict Mode's effect replay; object identity stops a stale promise painting.
  const artworkTarget = useRef<ArtworkTarget | null>(null);
  const committed = useRef<MovieDetail | null | undefined>(undefined);

  // The machine bumps commitSeq in the same update that drops the reel. Committing
  // during render keeps the reel unmount and the reveal in one paint. Seeded from
  // the current seq so a remount (tab switch) never replays a committed reveal.
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
    // Reuse the backdrop the machine already decoded when the current query still agrees.
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

  // Commit content without waiting on artwork, so a slow image cannot leave the title blank.
  useEffect(() => {
    if (isLoading) return;
    if (spinning) return; // the reel owns the transition; commit waits for the land

    const next = current ?? null;

    // Reload mid-spin: hand the draw to the machine first so the winner never flashes
    // ahead of the reel. Building the reel needs the pool, so wait for it.
    if (next?.drawnAt && !drawState.seen.includes(next.drawnAt)) {
      if (drawAwaitingReveal(next, resolveDrawEnv())) {
        if (pooled === undefined) return;
        drawStore.send({ type: "RESUME", current: next, pool: pooled });
        return; // the store subscription re-renders with the spin (or as seen)
      }
      drawStore.send({ type: "RESUME", current: next, pool: pooled ?? [] });
    }

    // Key the reveal on drawnAt + movieID, not object identity: every response carries
    // a fresh `serverNow`, so a no-op refetch would replay the reveal on each refocus.
    // `undefined` means nothing committed yet, distinct from a committed empty (null).
    const sameDraw =
      committed.current !== undefined &&
      (current?.drawnAt ?? null) === (committed.current?.drawnAt ?? null) &&
      (current?.movieID ?? null) === (committed.current?.movieID ?? null);
    if (sameDraw) {
      // Take late-arriving fields, but do not bump revealId: no re-animation for an unchanged draw.
      committed.current = current;
      setShown(next);
      return;
    }

    committed.current = current;
    setShown(next);
    setRevealId((n) => n + 1);
    // The pool/seen deps only re-run the resume check.
  }, [isLoading, current, spinning, pooled, drawState.seen]);

  // An Active wildcard takes over the Hero; the draw stays in `shown` for when it ends.
  const heroMovie = wildcard?.movie ?? shown;
  const desiredArtwork = artworkDescriptor(heroMovie);

  // Swap a changed draw to its procedural art before paint; a same-draw path change keeps the decoded layer.
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

  // Null for remote art, so a title-only change leaves an in-flight decode alone.
  const desiredArtworkFallback = desiredArtwork.url ? null : desiredArtwork.fallback;

  // Paint remote art only once decoded. The source key is stable across metadata-only refetches.
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

  useEffect(() => {
    if (marking && !shown) setMarking(false);
  }, [marking, shown]);

  useEffect(() => {
    if (drawing && shown) setDrawing(false);
  }, [drawing, shown]);

  const draw = shown;
  // False until the first commit, so no placeholder copy flashes before the real draw.
  const ready = revealId > 0;
  const hue = hueOf(heroMovie?.title ?? "moviepickarr");
  const canDraw = !draw && (pooled?.length ?? 0) > 0;
  const heroKey = wildcard ? `wildcard-${wildcard.id}` : `draw-${revealId}`;

  return (
    <>
    <section className="hero" data-ready={revealId > 0 ? "" : undefined}>
      <Backdrop bg={artwork.bg} revision={artwork.revision} />
      <div className="hero__inner">
        <div className="hero__poster" key={`p-${heroKey}`} style={ri(0)}>
          <Poster
            title={heroMovie?.title ?? "No draw yet"}
            hue={hue}
            posterPath={heroMovie?.posterPath}
            showTitle={ready && !heroMovie?.posterPath}
          />
        </div>

        <div className="hero__body" key={`b-${heroKey}`}>
          <div className={`hero__eyebrow eyebrow${wildcard ? " hero__eyebrow--wildcard" : ""}`} style={ri(1)}>
            {!ready ? (
              ""
            ) : wildcard ? (
              <>
                <AsteriskIcon />
                Active wildcard · added by{" "}
                {wildcard.movie.addedByArchived ? (
                  <span className="hero__by">{wildcard.movie.addedByName}</span>
                ) : (
                  <Link
                    to="/users"
                    search={{ member: wildcard.movie.addedByID }}
                    className="hero__by"
                    title={`See ${possessive(wildcard.movie.addedByName)} board`}
                  >
                    {wildcard.movie.addedByName}
                  </Link>
                )}
              </>
            ) : draw ? (
              <>
                {/* A push, unlike the modal's replace, so Back returns to the draw (#238). */}
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
            {!ready ? "" : (heroMovie?.title ?? "Draw next movie")}
          </h2>

          {/* Always rendered (reserved height in CSS) so the banner never re-lays-out. */}
          <p className="hero__tag" style={ri(3)}>
            {!ready
              ? null
              : heroMovie?.tagline
                ? `"${heroMovie.tagline}"`
                : heroMovie
                  ? null
                  : (pooled?.length ?? 0) > 0
                    ? "The pool is stocked. Spin for a random draw."
                    : "Add movies to the pool to get started."}
          </p>

          <div className="hero__meta" style={ri(4)}>
            {ready && heroMovie && <MetaChips movie={heroMovie} links={externalLinks(heroMovie)} />}
          </div>

          {ready && wildcard && draw && (
            <div className="hero__held-draw" style={ri(5)}>
              <span className="hero__held-label">Current draw on hold</span>
              <span aria-hidden="true">·</span>
              <button
                type="button"
                className="hero__held-title"
                onClick={() => heldDrawModal.open(draw)}
                title={`View ${draw.title} details`}
              >
                {draw.title}
              </button>
            </div>
          )}

          <div className="hero__actions" style={ri(6)}>
            {ready &&
              (wildcard ? (
                <button
                  type="button"
                  className="btn btn--accent"
                  onClick={() => {
                    if (!gate.guest) watchWildcardMutation.mutate(wildcard.id);
                  }}
                  disabled={watchWildcardMutation.isPending || cancelWildcardMutation.isPending}
                  aria-disabled={gate.guest || undefined}
                  aria-label={gate.guest ? `Mark as watched, ${guestWildcardTip}` : undefined}
                  title={gate.guest ? guestWildcardTip : undefined}
                  aria-busy={watchWildcardMutation.isPending || undefined}
                >
                  {watchWildcardMutation.isPending ? <Loader2Icon className="animate-spin mg-spin" /> : <EyeIcon />}
                  {watchWildcardMutation.isPending ? "Marking…" : "Mark as watched"}
                </button>
              ) : marking || drawing ? (
                <button type="button" className="btn btn--accent" disabled aria-busy="true">
                  <Loader2Icon className="animate-spin mg-spin" />
                  {marking ? "Marking…" : "Drawing…"}
                </button>
              ) : draw ? (
                // Disabled, not hidden, for spectators, so the tooltip names whose turn it is.
                <button
                  type="button"
                  className="btn btn--accent"
                  onClick={() => {
                    if (!gate.locked) watchMutation.mutate();
                  }}
                  disabled={!wildcardStateKnown || Boolean(wildcard)}
                  aria-disabled={gate.locked || undefined}
                  aria-label={gate.locked ? `Mark as watched, ${watchLockedTip(gate)}` : undefined}
                  title={wildcard
                    ? "Watch or cancel the Active wildcard first."
                    : !wildcardStateKnown
                      ? "Checking for an Active wildcard."
                      : gate.locked
                        ? watchLockedTip(gate)
                        : undefined}
                >
                  <EyeIcon />
                  Mark as watched
                </button>
              ) : (
                <button
                  type="button"
                  className="btn btn--accent"
                  onClick={() => {
                    if (!gate.locked) drawMutation.mutate();
                  }}
                  disabled={!canDraw}
                  aria-disabled={gate.locked || undefined}
                  aria-label={gate.locked ? `Draw random movie, ${drawLockedTip(gate)}` : undefined}
                  title={gate.locked ? drawLockedTip(gate) : undefined}
                >
                  <ShuffleIcon />
                  Draw random movie
                </button>
              ))}

            {ready && draw && !spinning && wildcardStateKnown && !wildcard && (
              <button
                type="button"
                className="btn btn--ghost hero__wildcard-open"
                onClick={() => {
                  if (!gate.guest) setWildcardPickerHostID(draw.movieID);
                }}
                aria-disabled={gate.guest || undefined}
                aria-label={gate.guest ? `Choose wildcard, ${guestWildcardTip}` : undefined}
                title={gate.guest ? guestWildcardTip : undefined}
              >
                <AsteriskIcon />
                Choose wildcard
              </button>
            )}

            {ready && draw && !spinning && wildcard && (
              <button
                type="button"
                className="btn btn--ghost hero__wildcard-open"
                onClick={() => {
                  if (!gate.guest) setWildcardCancelID(wildcard.id);
                }}
                disabled={watchWildcardMutation.isPending || cancelWildcardMutation.isPending}
                aria-disabled={gate.guest || undefined}
                aria-label={gate.guest ? `Cancel wildcard, ${guestWildcardTip}` : undefined}
                title={gate.guest ? guestWildcardTip : undefined}
              >
                <XIcon />
                Cancel wildcard
              </button>
            )}

            {ready && draw && !spinning && !wildcardStateKnown && wildcardQuery.isError && (
              <button type="button" className="btn btn--ghost hero__wildcard-open" onClick={() => void wildcardQuery.refetch()}>
                <RefreshCwIcon />
                Retry wildcard status
              </button>
            )}

            {ready && nextUp?.name && (
              <div className="hero__nextup">
                <Avatar name={nextUp.name} size={30} />
                <div className="nm">{gate.isSelf ? "Your turn" : `${possessive(gate.nextUpName)} turn`}</div>
                {/* Stays enabled while the confirm is pending so it can take focus back on close. */}
                {gate.canSkip && !spinning && !drawUnrevealed && (
                  <button
                    type="button"
                    className="iconbtn hero__skip"
                    onClick={() => setSkipHolder({ id: nextUp.id, name: nextUp.name })}
                    aria-label={`Skip ${possessive(nextUp.name)} turn`}
                    title={`Skip ${possessive(nextUp.name)} turn`}
                  >
                    <SkipForwardIcon />
                  </button>
                )}
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
    {wildcardPickerHostID !== null && (
      <WildcardModal hostMovieID={wildcardPickerHostID} onClose={() => setWildcardPickerHostID(null)} />
    )}
    {heldDrawModal.selected && (
      <MovieModal
        movie={draw?.movieID === heldDrawModal.selected.movieID ? draw : heldDrawModal.selected}
        open={heldDrawModal.isOpen}
        onRequestClose={heldDrawModal.close}
        onClose={heldDrawModal.onClosed}
      />
    )}
    <DeletionDialog
      isOpen={wildcardCancelID !== null}
      pending={cancelWildcardMutation.isPending}
      onClose={() => setWildcardCancelID(null)}
      onConfirm={() => {
        if (wildcardCancelID !== null) cancelWildcardMutation.mutate(wildcardCancelID);
      }}
      title="Cancel this wildcard?"
      description="The movie returns to its previous place. Its acquisition requirement closes, but moviepickarr does not remove anything from Radarr."
      confirmText="Cancel wildcard"
      pendingText="Canceling…"
      cancelText="Keep wildcard"
    />
    <DeletionDialog
      isOpen={skipHolder !== null}
      pending={skipMutation.isPending}
      onClose={() => setSkipHolder(null)}
      onConfirm={() => {
        if (skipHolder !== null) skipMutation.mutate(skipHolder.id);
      }}
      title={`Skip ${possessive(skipHolder?.name ?? "")} turn?`}
      description="Next up passes to the next member without a draw. You cannot undo a skip."
      confirmText="Skip turn"
      pendingText="Skipping…"
      cancelText="Keep turn"
    />
    </>
  );
}
