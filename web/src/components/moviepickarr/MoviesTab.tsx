import { useMutation, useQuery } from "@tanstack/react-query";
import { useVirtualizer } from "@tanstack/react-virtual";
import { LayoutGridIcon, ListIcon, LinkIcon, LockIcon, LockOpenIcon, PencilIcon, SearchIcon } from "lucide-react";
import { type KeyboardEvent as ReactKeyboardEvent, type ReactNode, useDeferredValue, useMemo, useRef, useState } from "react";

import { APIClient } from "@/api/APIClient";
import {
  MeQueryOptions,
  MoviesGetPoolQueryOptions,
  MoviesGetWatchedQueryOptions,
  SettingsGetPoolStateQueryOptions,
} from "@/api/queries";

import { EditMovieDialog } from "@/components/EditMovieDialog";
import { Avatar, AdderTag, Rating } from "@/components/moviepickarr/Bits";
import {
  dateTimeParts,
  hueOf,
  relativeDate,
  runtimeLabel,
  yearOf,
} from "@/components/moviepickarr/lib";
import { MovieModal } from "@/components/moviepickarr/MovieModal";
import { StatNumber } from "@/components/moviepickarr/numberRoll";
import { isSelf } from "@/components/moviepickarr/ownership";
import { canLockPool, ROUND_CLOSED, ROUND_OPEN } from "@/components/moviepickarr/poolLock";
import { Poster } from "@/components/moviepickarr/Poster";
import { chunkRows, filterWatched } from "@/components/moviepickarr/search";
import { toast } from "@/components/ui/toast-api";

import type { MovieTile } from "@/types/Response";

import { useToggle } from "@/hooks/hooks";
import { useFlipRail } from "@/hooks/useFlipRail";
import { useGridMetrics, virtualRowStyle } from "@/hooks/useGridMetrics";
import { useMovieModal } from "@/hooks/useMovieModalHistory";
import { documentScrollOwner } from "@/lib/scrollPolicy";

type WatchedView = "grid" | "list";

export function MoviesTab() {
  const { data: pooled, isPending: poolPending, isError: poolError } = useQuery(MoviesGetPoolQueryOptions());
  const { data: watched, isPending: watchedPending, isError: watchedError } = useQuery(MoviesGetWatchedQueryOptions());
  const { data: poolState } = useQuery(SettingsGetPoolStateQueryOptions());
  const isLocked = !!poolState?.poolLocked;
  const { data: me } = useQuery(MeQueryOptions());
  // Admin-only server-side; disabled, not hidden, like the turn gate's controls.
  const canLock = canLockPool(me?.role);

  // `poolEntries` keeps a removed tile until its exit finishes, so render from it, not `pooled`.
  const {
    containerRef: poolRef,
    entries: poolEntries,
    itemProps: poolItemProps,
  } = useFlipRail<MovieTile>(pooled ?? [], (m) => String(m.movieID));

  // Opening the modal pushes a history entry, so browser Back closes it (#196).
  const { selected, isOpen, open, close, onClosed } = useMovieModal();

  // Render from the live lists so SSE refetches (late enrichment) reach an open modal.
  const selectedLive = useMemo(() => {
    if (!selected) return null;
    return (
      (pooled ?? []).find((m) => m.movieID === selected.movieID) ??
      (watched ?? []).find((m) => m.movieID === selected.movieID) ??
      selected
    );
  }, [selected, pooled, watched]);

  const lockMutation = useMutation({
    mutationFn: () => APIClient.settings.toggleLock(!isLocked),
    onError: () => toast.error("Failed to toggle the pool lock"),
  });

  return (
    <>
      <section className="mg-rise">
        <div className="sec-head">
          <div className="sec-title">
            <h2>In the Pool</h2>
            <span className="sec-count">
              <StatNumber value={pooled?.length ?? 0} /> locked in · {isLocked ? ROUND_CLOSED : ROUND_OPEN}
            </span>
          </div>
          <div className="watched-controls">
            <button
              type="button"
              className="btn btn--ghost btn--sm"
              onClick={() => lockMutation.mutate()}
              disabled={!canLock || lockMutation.isPending}
              title={canLock ? undefined : "Only admins can lock the pool."}
            >
              {isLocked ? <LockOpenIcon /> : <LockIcon />}
              {isLocked ? "Unlock pool" : "Lock pool"}
            </button>
          </div>
        </div>

        {poolError ? (
          <p className="empty text-destructive">Failed to load the pool.</p>
        ) : poolPending ? (
          <p className="empty">Loading pool…</p>
        ) : poolEntries.length > 0 ? (
          <div className="tile-grid tile-grid--pool" ref={poolRef}>
            {poolEntries.map(({ key, item: movie, exiting }) => (
              <article
                className="tile"
                key={key}
                data-flip-exit={exiting || undefined}
                {...poolItemProps(key)}
                {...openProps(() => open(movie))}
              >
                <Poster
                  title={movie.title}
                  hue={hueOf(movie.title)}
                  posterPath={movie.posterPath}
                  sizes="auto, 342px"
                />
                <div className="t-meta">
                  <span className="t-title">{movie.title}</span>
                  <div className="t-sub">
                    <AdderTag name={movie.addedByName} />
                  </div>
                </div>
              </article>
            ))}
          </div>
        ) : (
          <p className="empty">No movies in the pool</p>
        )}
      </section>

      <div className="rule" />

      <WatchedSection
        watched={watched}
        isPending={watchedPending}
        isError={watchedError}
        onOpen={open}
      />

      {selectedLive && (
        <MovieModal
          movie={selectedLive}
          open={isOpen}
          onRequestClose={close}
          onClose={onClosed}
        />
      )}
    </>
  );
}

/** Interactive props that open a movie's detail modal from a poster tile. */
function openProps(onOpen: () => void) {
  return {
    role: "button" as const,
    tabIndex: 0,
    onClick: onOpen,
    onKeyDown: (e: ReactKeyboardEvent) => {
      if (e.key === "Enter" || e.key === " ") {
        e.preventDefault();
        onOpen();
      }
    },
  };
}

/** Owns the search state, not MoviesTab, so a keystroke does not re-render the pool rail. */
function WatchedSection({
  watched,
  isPending,
  isError,
  onOpen,
}: {
  watched: MovieTile[] | undefined;
  isPending: boolean;
  isError: boolean;
  onOpen: (movie: MovieTile) => void;
}) {
  const [search, setSearch] = useState("");
  const deferredSearch = useDeferredValue(search);
  const [view, setView] = useState<WatchedView>("grid");

  const filtered = useMemo(() => filterWatched(watched ?? [], deferredSearch), [watched, deferredSearch]);
  const total = watched?.length ?? 0;

  return (
    <section className="mg-rise">
      <div className="sec-head">
        <div className="sec-title">
          <h2>Watched</h2>
          <span className="sec-count">
            {deferredSearch ? (
              <>
                <StatNumber value={filtered.length} />/<StatNumber value={total} /> movies
              </>
            ) : (
              <>
                <StatNumber value={total} /> {total === 1 ? "movie" : "movies"}
              </>
            )}
          </span>
        </div>
        <div className="watched-controls">
          <label className="field watched-controls__search">
            <SearchIcon />
            <input
              name="watched-search"
              aria-label="Search watched movies by title or adder"
              placeholder="Search by title or adder…"
              value={search}
              onChange={(e) => setSearch(e.target.value)}
            />
          </label>
          <div className="seg">
            <button type="button" data-active={view === "grid"} onClick={() => setView("grid")} aria-label="Grid view">
              <LayoutGridIcon />
            </button>
            <button type="button" data-active={view === "list"} onClick={() => setView("list")} aria-label="List view">
              <ListIcon />
            </button>
          </div>
        </div>
      </div>

      {isError ? (
        <p className="empty text-destructive">Failed to load watched movies.</p>
      ) : isPending ? (
        <p className="empty">Loading watched…</p>
      ) : filtered.length === 0 ? (
        <p className="empty">{deferredSearch ? "No movies match your search" : "No movies watched yet"}</p>
      ) : view === "grid" ? (
        <VirtualWatched
          key="grid"
          className="watch-body tile-grid"
          movies={filtered}
          estimateSize={300}
          render={(movie) => (
            <article className="tile" key={movie.movieID} {...openProps(() => onOpen(movie))}>
              <Poster
                title={movie.title}
                hue={hueOf(movie.title)}
                posterPath={movie.posterPath}
                sizes="auto, 342px"
              />
              <div className="t-meta">
                <span className="t-title">{movie.title}</span>
                <div className="t-sub">
                  <span className="flex min-w-0 items-center gap-2">
                    <Avatar name={movie.addedByName} size={20} />
                    <span className="t-date">{relativeDate(movie.watchedAt)}</span>
                  </span>
                  {movie.wildcardOfMovieId && <span className="wildcard-mark">Wildcard</span>}
                </div>
              </div>
            </article>
          )}
        />
      ) : (
        <VirtualWatched
          key="list"
          className="watch-body watch-list"
          movies={filtered}
          estimateSize={84}
          render={(movie) => <WatchedRow key={movie.movieID} movie={movie} onOpen={() => onOpen(movie)} />}
        />
      )}
    </section>
  );
}

/**
 * The watched grid/list, virtualized against the document scroll. Layout stays in CSS:
 * `useGridMetrics` reads back the resolved columns and gaps, so breakpoints are not restated in JS.
 */
function VirtualWatched({
  className,
  movies,
  estimateSize,
  render,
}: {
  className: string;
  movies: readonly MovieTile[];
  /** Starting row height in px; real heights are measured once rendered. */
  estimateSize: number;
  render: (movie: MovieTile) => ReactNode;
}) {
  const containerRef = useRef<HTMLDivElement>(null);
  const { template, lanes, columnGap, rowGap, offsetTop } = useGridMetrics(containerRef);
  const rows = useMemo(() => chunkRows(movies, lanes), [movies, lanes]);

  const virtualizer = useVirtualizer({
    count: rows.length,
    getScrollElement: documentScrollOwner,
    estimateSize: () => estimateSize,
    overscan: 3,
    gap: rowGap,
    scrollMargin: offsetTop,
  });

  return (
    <div
      ref={containerRef}
      className={className}
      style={{ position: "relative", height: virtualizer.getTotalSize() }}
    >
      {virtualizer.getVirtualItems().map((row) => (
        <div
          key={row.key}
          data-index={row.index}
          ref={virtualizer.measureElement}
          style={{
            ...virtualRowStyle(row.start - offsetTop),
            display: "grid",
            gridTemplateColumns: template,
            columnGap,
          }}
        >
          {rows[row.index].map(render)}
        </div>
      ))}
    </div>
  );
}

function WatchedRow({ movie, onOpen }: { movie: MovieTile; onOpen: () => void }) {
  const [editOpen, toggleEdit] = useToggle(false);
  const { date, time } = dateTimeParts(movie.watchedAt);
  // Editing is adder-only server-side, with no admin override.
  const { data: me } = useQuery(MeQueryOptions());
  const canEdit = isSelf(me?.id, movie.addedByID);

  const editMutation = useMutation({
    mutationFn: (payload: { title: string; link: string; watchedAt?: string }) =>
      APIClient.board.updateMovie(movie.movieID, payload.title, payload.link, payload.watchedAt),
    onSuccess: () => {
      toast.success(`${movie.title} updated`);
      toggleEdit();
    },
    onError: () => toast.error("Failed to update movie"),
  });

  const year = yearOf(movie.releaseDate);
  const runtime = runtimeLabel(movie.runtime);
  const sub = [year, runtime, `added by ${movie.addedByName}`].filter(Boolean).join(" · ");

  return (
    <>
      <EditMovieDialog
        isOpen={editOpen}
        onClose={toggleEdit}
        initialTitle={movie.title}
        initialLink={movie.link}
        initialWatchedAt={movie.watchedAt}
        allowWatchedAtEdit
        isSaving={editMutation.isPending}
        onSubmit={(payload) => editMutation.mutate(payload)}
      />

      <div className="wrow">
        <button type="button" className="wr-open" onClick={onOpen} aria-label={`Details for ${movie.title}`}>
          <Poster
            title={movie.title}
            hue={hueOf(movie.title)}
            posterPath={movie.posterPath}
            showTitle={false}
            sizes="auto, 44px"
          />
          <div className="wr-main">
            <span className="wr-title">
              {movie.title}
              <Rating voteAverage={movie.voteAverage} />
              {movie.wildcardOfMovieId && <span className="wildcard-mark">Wildcard</span>}
            </span>
            <span className="wr-sub">{sub}</span>
          </div>
        </button>
        <div className="wr-actions">
          {movie.link && (
            <a className="iconbtn" href={movie.link} target="_blank" rel="noopener noreferrer" aria-label="Open link">
              <LinkIcon />
            </a>
          )}
          {canEdit && (
            <button type="button" className="iconbtn" onClick={toggleEdit} aria-label="Edit">
              <PencilIcon />
            </button>
          )}
        </div>
        <div className="wr-date">
          {date}
          <br />
          {time}
        </div>
      </div>
    </>
  );
}
