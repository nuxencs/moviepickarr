import { useQuery } from "@tanstack/react-query";
import { useVirtualizer } from "@tanstack/react-virtual";
import { ChevronRightIcon, SearchIcon } from "lucide-react";
import { memo, useCallback, useDeferredValue, useId, useLayoutEffect, useMemo, useRef, useState } from "react";

import { ApiError } from "@/api/APIClient";
import { MovieLibraryQueryOptions } from "@/api/queries";

import { MovieCorrectionDialog } from "@/components/moviepickarr/admin/MovieCorrectionDialog";
import { hueOf, plural, yearOf } from "@/components/moviepickarr/lib";
import { Poster } from "@/components/moviepickarr/Poster";

import type { LibraryMovie, MovieStatus } from "@/types/Response";

import { virtualRowStyle } from "@/hooks/useGridMetrics";
import { documentOffsetTop, pageScrollOwner } from "@/lib/scrollPolicy";


import "@/pages/admin-maintenance.css";

const STATUS_LABELS: Record<MovieStatus, string> = {
  stash: "Stash",
  pool: "Pool",
  current: "Current draw",
  wildcard: "Wildcard",
  watched: "Watched",
};

const titleCollator = new Intl.Collator(undefined, { sensitivity: "base", numeric: true });

const LibraryRow = memo(function LibraryRow({
  movie,
  onOpen,
  index,
  setSize,
  top,
  measure,
}: {
  movie: LibraryMovie;
  onOpen: (movieID: number) => void;
  index: number;
  setSize: number;
  top: number;
  measure: (node: HTMLLIElement | null) => void;
}) {
  const factsID = useId();
  const year = yearOf(movie.releaseDate);

  return (
    <li
      ref={measure}
      data-index={index}
      className="library-register__item"
      style={virtualRowStyle(top)}
      aria-setsize={setSize}
      aria-posinset={index + 1}
    >
      <button
        type="button"
        className="library-row"
        aria-haspopup="dialog"
        aria-label={`Correct ${movie.title}`}
        aria-describedby={factsID}
        onClick={() => onOpen(movie.movieID)}
      >
        <Poster
          title={movie.title}
          hue={hueOf(movie.title)}
          posterPath={movie.posterPath}
          showTitle={false}
          sizes="auto, 44px"
        />
        <span className="library-row__movie">
          <strong>{movie.title}</strong>
          <span>{year ?? "Year unknown"}</span>
        </span>
        <span id={factsID} className="vis-hidden">
          {[
            STATUS_LABELS[movie.status],
            `added by ${movie.addedByName}`,
            movie.tmdbId ? `TMDB ${movie.tmdbId}` : "no TMDB id",
          ].join(", ")}
        </span>
        <span className="library-row__facts" aria-hidden="true">
          <span className="library-row__status" data-status={movie.status}>
            {STATUS_LABELS[movie.status]}
          </span>
          <span className="library-row__adder">{movie.addedByName}</span>
          <span className="library-row__tmdb mono">{movie.tmdbId ?? "No TMDB id"}</span>
        </span>
        <ChevronRightIcon className="library-row__chevron" aria-hidden="true" />
      </button>
    </li>
  );
});

/**
 * The library grows with every movie added, so it is virtualized (DESIGN.md §4).
 * The scroll owner is the Admin body at 901px and up and the document below it.
 */
function LibraryList({
  movies,
  onOpen,
}: {
  movies: readonly LibraryMovie[];
  onOpen: (movieID: number) => void;
}) {
  const listRef = useRef<HTMLUListElement>(null);
  const [scroll, setScroll] = useState<{ owner: HTMLElement | null; offset: number }>({
    owner: null,
    offset: 0,
  });

  useLayoutEffect(() => {
    const list = listRef.current;
    if (!list) return;
    const read = () => {
      const owner = pageScrollOwner(list);
      const offset = documentOffsetTop(list, owner);
      setScroll((prev) => (prev.owner === owner && prev.offset === offset ? prev : { owner, offset }));
    };
    read();
    const observer = new ResizeObserver(read);
    observer.observe(list);
    observer.observe(document.getElementById("root") ?? document.body);
    return () => observer.disconnect();
  }, []);

  const virtualizer = useVirtualizer({
    count: movies.length,
    getScrollElement: () => scroll.owner,
    estimateSize: () => 81,
    overscan: 6,
    scrollMargin: scroll.offset,
  });

  return (
    <ul
      ref={listRef}
      className="library-register__list"
      aria-label="Movie library"
      style={{ position: "relative", height: virtualizer.getTotalSize() }}
    >
      {virtualizer.getVirtualItems().map((item) => {
        const movie = movies[item.index];
        return (
          <LibraryRow
            key={movie.movieID}
            movie={movie}
            onOpen={onOpen}
            index={item.index}
            setSize={movies.length}
            top={item.start - scroll.offset}
            measure={virtualizer.measureElement}
          />
        );
      })}
    </ul>
  );
}

/** The admin Maintenance tab: every movie, and the Movie correction (ADR 0013). */
export function AdminMaintenancePage() {
  const library = useQuery(MovieLibraryQueryOptions());
  const [query, setQuery] = useState("");
  const deferredQuery = useDeferredValue(query);
  const [selectedID, setSelectedID] = useState<number | null>(null);
  const openMovie = useCallback((movieID: number) => setSelectedID(movieID), []);

  const movies = useMemo(
    () => [...(library.data ?? [])].sort((a, b) => titleCollator.compare(a.title, b.title)),
    [library.data],
  );
  const visible = useMemo(() => {
    const needle = deferredQuery.trim().toLocaleLowerCase();
    if (!needle) return movies;
    return movies.filter(
      (movie) =>
        movie.title.toLocaleLowerCase().includes(needle) ||
        movie.addedByName.toLocaleLowerCase().includes(needle),
    );
  }, [movies, deferredQuery]);
  // Resolved from the live list, so a deleted movie closes its dialog.
  const selected = movies.find((movie) => movie.movieID === selectedID);

  return (
    <section className="admin-section admin-maintenance mg-rise" aria-labelledby="admin-maintenance-title">
      <div className="sec-head">
        <div className="sec-title">
          <h2 id="admin-maintenance-title">Movie library</h2>
          {library.data && <span className="sec-count">{plural(library.data.length, "movie")}</span>}
        </div>
      </div>

      <p className="library-lede">
        Titles come from TMDB. Correct a movie that points at the wrong film or has the
        wrong watched date.
      </p>

      <label className="field library-search">
        <SearchIcon />
        <input
          type="search"
          name="library-search"
          aria-label="Search the movie library"
          value={query}
          onChange={(event) => setQuery(event.target.value)}
          placeholder="Search by title or adder"
        />
      </label>

      {library.isPending ? (
        <div className="adm-state" role="status">
          Loading the movie library…
        </div>
      ) : library.isError ? (
        <div className="adm-state" role="alert">
          {library.error instanceof ApiError && library.error.status === 403
            ? "Admin access is required to view the movie library."
            : "Failed to load the movie library."}
        </div>
      ) : visible.length === 0 ? (
        <div className="adm-state">
          {movies.length === 0 ? "The library has no movies yet." : "No movies match the search."}
        </div>
      ) : (
        <div className="library-register">
          <div className="library-register__head" aria-hidden="true">
            <span />
            <span>Movie</span>
            <span>Status</span>
            <span>Added by</span>
            <span>TMDB</span>
            <span />
          </div>
          <LibraryList movies={visible} onOpen={openMovie} />
        </div>
      )}

      {selected && <MovieCorrectionDialog movie={selected} onClose={() => setSelectedID(null)} />}
    </section>
  );
}
