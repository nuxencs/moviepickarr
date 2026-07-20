import { useMutation, useQuery } from "@tanstack/react-query";
import { LayoutGridIcon, ListIcon, LinkIcon, LockIcon, LockOpenIcon, PencilIcon, SearchIcon } from "lucide-react";
import { type KeyboardEvent as ReactKeyboardEvent, useMemo, useState } from "react";

import { APIClient } from "@/api/APIClient";
import {
  MeQueryOptions,
  MoviesGetPoolQueryOptions,
  MoviesGetWatchedQueryOptions,
  SettingsGetPoolLockQueryOptions,
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
import { Poster } from "@/components/moviepickarr/Poster";
import { toast } from "@/components/ui/toast-api";

import type { Movie } from "@/types/Response";

import { useToggle } from "@/hooks/hooks";
import { useFlipRail } from "@/hooks/useFlipRail";

type WatchedView = "grid" | "list";

export function MoviesTab() {
  const { data: pooled, isPending: poolPending, isError: poolError } = useQuery(MoviesGetPoolQueryOptions());
  const { data: watched, isPending: watchedPending, isError: watchedError } = useQuery(MoviesGetWatchedQueryOptions());
  const { data: isLocked } = useQuery(SettingsGetPoolLockQueryOptions());

  // FLIP enter/exit + glide for the pool grid — the same hook the Stats rails
  // use. Tiles fade in when a movie is added/promoted, fade out when it's drawn
  // (on reel-land), moved to the stash, or deleted; survivors glide to close the
  // gap. `poolEntries` includes a just-removed tile until its exit finishes, so
  // the grid render maps over it (not `pooled`) and gates on its length.
  const {
    containerRef: poolRef,
    entries: poolEntries,
    itemProps: poolItemProps,
  } = useFlipRail<Movie>(pooled ?? [], (m) => String(m.movieID));

  const [search, setSearch] = useState("");
  const [view, setView] = useState<WatchedView>("grid");
  const [selected, setSelected] = useState<Movie | null>(null);

  // The modal renders from the live lists so SSE-driven refetches (enrichment
  // lands seconds after an add) flow into an open modal; the stored object is
  // only the fallback while the movie momentarily sits in neither list.
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

  // Interactive props that open the detail modal from a poster tile.
  const tileProps = (movie: Movie) => ({
    role: "button" as const,
    tabIndex: 0,
    onClick: () => setSelected(movie),
    onKeyDown: (e: ReactKeyboardEvent) => {
      if (e.key === "Enter" || e.key === " ") {
        e.preventDefault();
        setSelected(movie);
      }
    },
  });

  const filteredWatched = useMemo(() => {
    const q = search.trim().toLowerCase();
    return (watched ?? []).filter(
      (m) => !q || m.title.toLowerCase().includes(q) || m.addedByName.toLowerCase().includes(q),
    );
  }, [watched, search]);

  return (
    <>
      {/* ---- In the Pool ---- */}
      <section className="block">
        <div className="sec-head">
          <div className="sec-title">
            <h2>In the Pool</h2>
            <span className="sec-count">
              <StatNumber value={pooled?.length ?? 0} /> locked in · round {isLocked ? "closed" : "open"}
            </span>
          </div>
          <div className="watched-controls">
            <button
              type="button"
              className="btn btn--ghost btn--sm"
              onClick={() => lockMutation.mutate()}
              disabled={lockMutation.isPending}
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
                {...tileProps(movie)}
              >
                <Poster
                  title={movie.title}
                  hue={hueOf(movie.title)}
                  posterPath={movie.posterPath}
                  voteAverage={movie.voteAverage}
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

      {/* ---- Watched ---- */}
      <section className="block">
        <div className="sec-head">
          <div className="sec-title">
            <h2>Watched</h2>
            <span className="sec-count">
              {search ? (
                <>
                  <StatNumber value={filteredWatched.length} />/<StatNumber value={watched?.length ?? 0} /> films
                </>
              ) : (
                <>
                  <StatNumber value={watched?.length ?? 0} /> {(watched?.length ?? 0) === 1 ? "film" : "films"}
                </>
              )}
            </span>
          </div>
          <div className="watched-controls">
            <label className="field watched-controls__search">
              <SearchIcon />
              <input
                name="watched-search"
                aria-label="Search watched films by title or adder"
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

        {watchedError ? (
          <p className="empty text-destructive">Failed to load watched movies.</p>
        ) : watchedPending ? (
          <p className="empty">Loading watched…</p>
        ) : filteredWatched.length === 0 ? (
          <p className="empty">{search ? "No films match your search" : "No movies watched yet"}</p>
        ) : view === "grid" ? (
          <div className="watch-body tile-grid" key="grid">
            {filteredWatched.map((movie) => (
              <article className="tile" key={movie.movieID} {...tileProps(movie)}>
                <Poster
                  title={movie.title}
                  hue={hueOf(movie.title)}
                  posterPath={movie.posterPath}
                  voteAverage={movie.voteAverage}
                />
                <div className="t-meta">
                  <span className="t-title">{movie.title}</span>
                  <div className="t-sub">
                    <span className="flex min-w-0 items-center gap-2">
                      <Avatar name={movie.addedByName} size={20} />
                      <span className="t-date">{relativeDate(movie.watchedAt)}</span>
                    </span>
                  </div>
                </div>
              </article>
            ))}
          </div>
        ) : (
          <div className="watch-body watch-list" key="list">
            {filteredWatched.map((movie) => (
              <WatchedRow key={movie.movieID} movie={movie} onOpen={() => setSelected(movie)} />
            ))}
          </div>
        )}
      </section>

      {selectedLive && <MovieModal movie={selectedLive} onClose={() => setSelected(null)} />}
    </>
  );
}

function WatchedRow({ movie, onOpen }: { movie: Movie; onOpen: () => void }) {
  const [editOpen, toggleEdit] = useToggle(false);
  const { date, time } = dateTimeParts(movie.watchedAt);
  // Editing is adder-only server-side (no admin override), so only the adder
  // gets the edit control; everyone else sees the watched row read-only. Same
  // ownership rule as the board, via isSelf (see ownership.ts).
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
          <Poster title={movie.title} hue={hueOf(movie.title)} posterPath={movie.posterPath} showTitle={false} />
          <div className="wr-main">
            <span className="wr-title">
              {movie.title}
              <Rating voteAverage={movie.voteAverage} />
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
