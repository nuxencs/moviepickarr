import { useMutation, useQuery } from "@tanstack/react-query";
import { LayoutGridIcon, ListIcon, LinkIcon, LockIcon, LockOpenIcon, PencilIcon, SearchIcon } from "lucide-react";
import { type KeyboardEvent as ReactKeyboardEvent, useMemo, useState } from "react";

import { APIClient } from "@/api/APIClient";
import {
  MoviesGetPoolQueryOptions,
  MoviesGetWatchedQueryOptions,
  SettingsGetPoolLockQueryOptions,
} from "@/api/queries";

import { EditMovieDialog } from "@/components/EditMovieDialog";
import { Avatar, PickerTag, Rating } from "@/components/movie-gang/Bits";
import { dateTimeParts, hueOf, relativeDate, runtimeLabel, yearOf } from "@/components/movie-gang/lib";
import { MovieModal } from "@/components/movie-gang/MovieModal";
import { Poster } from "@/components/movie-gang/Poster";
import { toast } from "@/components/ui/toast";

import type { Movie } from "@/types/Response";

import { useToggle } from "@/hooks/hooks";

type WatchedView = "grid" | "list";

export function MoviesTab() {
  const { data: pooled } = useQuery(MoviesGetPoolQueryOptions());
  const { data: watched } = useQuery(MoviesGetWatchedQueryOptions());
  const { data: isLocked } = useQuery(SettingsGetPoolLockQueryOptions());

  const [search, setSearch] = useState("");
  const [view, setView] = useState<WatchedView>("grid");
  const [selected, setSelected] = useState<Movie | null>(null);

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
    if (!q) return watched ?? [];
    return (watched ?? []).filter(
      (m) => m.title.toLowerCase().includes(q) || m.addedByName.toLowerCase().includes(q),
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
              {pooled?.length ?? 0} locked in · round {isLocked ? "closed" : "open"}
            </span>
          </div>
          <button
            type="button"
            className="btn btn--ghost btn--sm"
            onClick={() => lockMutation.mutate()}
            disabled={lockMutation.isPending}
          >
            {isLocked ? <LockOpenIcon /> : <LockIcon />}
            {isLocked ? "Unlock Pool" : "Lock Pool"}
          </button>
        </div>

        {pooled && pooled.length > 0 ? (
          <div className="tile-grid tile-grid--pool">
            {pooled.map((movie) => (
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
                    <PickerTag name={movie.addedByName} />
                  </div>
                </div>
              </article>
            ))}
          </div>
        ) : (
          <p className="py-6 text-center text-ink-3">No movies in the pool</p>
        )}
      </section>

      <div className="rule" />

      {/* ---- Watched ---- */}
      <section className="block">
        <div className="sec-head">
          <div className="sec-title">
            <h2>Watched</h2>
            <span className="sec-count">
              {search ? `${filteredWatched.length}/${watched?.length ?? 0}` : watched?.length ?? 0} films
            </span>
          </div>
          <div className="flex items-stretch gap-3">
            <label className="field" style={{ width: 230 }}>
              <SearchIcon />
              <input
                placeholder="Search by title or picker…"
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

        {filteredWatched.length === 0 ? (
          <p className="py-6 text-center text-ink-3">
            {search ? "No films match your search" : "No movies watched yet"}
          </p>
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

      {selected && <MovieModal movie={selected} onClose={() => setSelected(null)} />}
    </>
  );
}

function WatchedRow({ movie, onOpen }: { movie: Movie; onOpen: () => void }) {
  const [editOpen, toggleEdit] = useToggle(false);
  const { date, time } = dateTimeParts(movie.watchedAt);

  const editMutation = useMutation({
    mutationFn: (payload: { title: string; link: string; watchedAt?: string }) =>
      APIClient.users.updateMovie(movie.addedByID, movie.movieID, payload.title, payload.link, payload.watchedAt),
    onSuccess: () => {
      toast.success(`${movie.title} updated`);
      toggleEdit();
    },
    onError: () => toast.error("Error updating movie"),
  });

  const year = yearOf(movie.releaseDate);
  const runtime = runtimeLabel(movie.runtime);
  const sub = [year, runtime, `picked by ${movie.addedByName}`].filter(Boolean).join(" · ");

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
        <button type="button" className="contents cursor-pointer text-left" onClick={onOpen} aria-label={`Details for ${movie.title}`}>
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
          <button type="button" className="iconbtn" onClick={toggleEdit} aria-label="Edit">
            <PencilIcon />
          </button>
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
