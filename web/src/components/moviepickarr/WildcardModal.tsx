/*
 * Direction contract: extend the existing editorial movie browser. Preserve
 * its flat poster grid, compact type, warm accent, and direct action language.
 */
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { AsteriskIcon, Loader2Icon, SearchIcon, XIcon } from "lucide-react";
import { type FormEvent, useEffect, useMemo, useRef, useState } from "react";

import { APIClient } from "@/api/APIClient";
import { UsersGetAllQueryOptions } from "@/api/queries";
import { MoviesKeys, UsersKeys } from "@/api/query_keys";

import { hueOf, yearOf } from "@/components/moviepickarr/lib";
import { Modal } from "@/components/moviepickarr/Modal";
import { Poster } from "@/components/moviepickarr/Poster";
import { toast } from "@/components/ui/toast-api";

import type { MovieTile, TMDBMovie } from "@/types/Response";

interface WildcardModalProps {
  hostMovieID: number;
  onClose: () => void;
}

export function WildcardModal({ hostMovieID, onClose }: WildcardModalProps) {
  const queryClient = useQueryClient();
  const { data: members } = useQuery(UsersGetAllQueryOptions());
  const [query, setQuery] = useState("");
  const [searchedTerm, setSearchedTerm] = useState("");
  const [results, setResults] = useState<TMDBMovie[]>([]);
  const [hasSearched, setHasSearched] = useState(false);
  const [isSearching, setIsSearching] = useState(false);
  const [pendingKey, setPendingKey] = useState<string | null>(null);
  const inputRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    const timeout = window.setTimeout(() => inputRef.current?.focus(), 60);
    return () => window.clearTimeout(timeout);
  }, []);

  const library = useMemo(() => {
    const byID = new Map<number, MovieTile>();
    for (const member of members ?? []) {
      for (const movie of [...Object.values(member.currentPool), ...Object.values(member.stash)]) {
        byID.set(movie.movieID, movie);
      }
    }
    return [...byID.values()].sort((a, b) => a.title.localeCompare(b.title));
  }, [members]);

  const localMatches = useMemo(() => {
    const term = query.trim().toLocaleLowerCase();
    if (!term) return library;
    return library.filter((movie) => movie.title.toLocaleLowerCase().includes(term));
  }, [library, query]);

  const handleSearch = async (event: FormEvent) => {
    event.preventDefault();
    const term = query.trim();
    if (!term || isSearching) return;
    setIsSearching(true);
    setHasSearched(true);
    setSearchedTerm(term);
    try {
      setResults(await APIClient.tmdb.search(term));
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "Movie search failed");
      setResults([]);
    } finally {
      setIsSearching(false);
    }
  };

  const finishSelection = async (title: string, select: () => Promise<unknown>) => {
    try {
      await select();
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: MoviesKeys.wildcard() }),
        queryClient.invalidateQueries({ queryKey: MoviesKeys.listpool() }),
        queryClient.invalidateQueries({ queryKey: UsersKeys.list() }),
      ]);
      toast.success(`${title} is the wildcard`);
      onClose();
    } catch (error) {
      toast.error(error instanceof Error ? error.message : `Failed to select ${title}`);
    } finally {
      setPendingKey(null);
    }
  };

  const chooseLibraryMovie = (movie: MovieTile) => {
    if (pendingKey) return;
    setPendingKey(`local-${movie.movieID}`);
    void finishSelection(movie.title, () => APIClient.movies.selectWildcard(hostMovieID, movie.movieID));
  };

  const chooseTMDBMovie = (movie: TMDBMovie) => {
    if (pendingKey) return;
    setPendingKey(`tmdb-${movie.id}`);
    void finishSelection(movie.title, () => APIClient.movies.selectTMDBWildcard(hostMovieID, movie.title, movie.id));
  };

  return (
    <Modal label="Choose a wildcard" onClose={onClose} dismissible={pendingKey === null} capped className="modal--wildcard">
      {(close) => (
        <>
          <div className="modal__head">
            <div className="top">
              <div>
                <h3>Choose a wildcard</h3>
                <p>The Current draw and next member stay in place.</p>
              </div>
              <button type="button" className="iconbtn" onClick={close} aria-label="Close" disabled={pendingKey !== null}>
                <XIcon />
              </button>
            </div>
            <form className="modal__search" onSubmit={handleSearch}>
              <label className="field">
                <SearchIcon />
                <input
                  ref={inputRef}
                  name="wildcard-search"
                  aria-label="Search the library and TMDB"
                  value={query}
                  onChange={(event) => setQuery(event.target.value)}
                  placeholder="Search the library or TMDB…"
                  disabled={isSearching || pendingKey !== null}
                />
              </label>
              <button type="submit" className="btn btn--accent" disabled={!query.trim() || isSearching || pendingKey !== null}>
                {isSearching ? <Loader2Icon className="animate-spin mg-spin" /> : <SearchIcon />}
                {isSearching ? "Searching…" : "Search TMDB"}
              </button>
            </form>
          </div>

          <div className="modal__scroll wildcard-picker">
            <section className="wildcard-picker__section" aria-labelledby="wildcard-library-title">
              <div className="wildcard-picker__heading">
                <h4 id="wildcard-library-title">In the library</h4>
                <span>{localMatches.length}</span>
              </div>
              {localMatches.length > 0 ? (
                <div className="result-grid">
                  {localMatches.map((movie) => {
                    const busy = pendingKey === `local-${movie.movieID}`;
                    return (
                      <WildcardResult
                        key={movie.movieID}
                        title={movie.title}
                        posterPath={movie.posterPath}
                        year={yearOf(movie.releaseDate)}
                        detail={`Added by ${movie.addedByName}`}
                        busy={busy}
                        disabled={pendingKey !== null}
                        onChoose={() => chooseLibraryMovie(movie)}
                      />
                    );
                  })}
                </div>
              ) : (
                <p className="empty">No library movies match this title.</p>
              )}
            </section>

            {hasSearched && (
              <section className="wildcard-picker__section" aria-labelledby="wildcard-tmdb-title">
                <div className="wildcard-picker__heading">
                  <h4 id="wildcard-tmdb-title">From TMDB</h4>
                  <span>{isSearching ? "Searching" : `${results.length} for “${searchedTerm}”`}</span>
                </div>
                {!isSearching && results.length === 0 ? (
                  <p className="empty">No TMDB matches found. Try a broader title.</p>
                ) : (
                  <div className="result-grid">
                    {results.map((movie) => {
                      const busy = pendingKey === `tmdb-${movie.id}`;
                      return (
                        <WildcardResult
                          key={movie.id}
                          title={movie.title}
                          posterPath={movie.poster_path ?? undefined}
                          year={yearOf(movie.release_date)}
                          detail={movie.overview || "No overview available."}
                          busy={busy}
                          disabled={pendingKey !== null}
                        onChoose={() => chooseTMDBMovie(movie)}
                        />
                      );
                    })}
                  </div>
                )}
              </section>
            )}
          </div>
        </>
      )}
    </Modal>
  );
}

function WildcardResult({
  title,
  posterPath,
  year,
  detail,
  busy,
  disabled,
  onChoose,
}: {
  title: string;
  posterPath?: string;
  year?: number;
  detail: string;
  busy: boolean;
  disabled: boolean;
  onChoose: () => void;
}) {
  return (
    <article className="result">
      <div className="result__media">
        <Poster title={title} hue={hueOf(title)} posterPath={posterPath} />
        <div className="r-overlay" data-busy={busy}>
          <div>
            <div className="r-overlay__title">{title}</div>
            <div className="r-overlay__year">{year ?? "Year unavailable"}</div>
            <p className="r-overlay__desc">{detail}</p>
          </div>
          <button type="button" className="btn btn--accent btn--sm" onClick={onChoose} disabled={disabled}>
            {busy ? <Loader2Icon className="animate-spin mg-spin" /> : <AsteriskIcon />}
            {busy ? "Choosing…" : "Choose"}
          </button>
        </div>
      </div>
      <div className="r-info">
        <span className="r-title">{title}</span>
        <span className="r-year">{year ?? "Year unavailable"}</span>
      </div>
    </article>
  );
}
