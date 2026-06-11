import { Loader2Icon, PlusIcon, SearchIcon, XIcon } from "lucide-react";
import { FormEvent, useEffect, useRef, useState } from "react";

import { APIClient } from "@/api/APIClient";

import { hueOf, yearOf } from "@/components/movie-gang/lib";
import { Modal } from "@/components/movie-gang/Modal";
import { Poster } from "@/components/movie-gang/Poster";
import { toast } from "@/components/ui/toast";

import type { TMDBMovie } from "@/types/Response";

interface SearchModalProps {
  userID: number;
  userName: string;
  onClose: () => void;
}

/** Flat, editorial TMDB search modal. Real posters; hover reveals overview + add. */
export function SearchModal({ userID, userName, onClose }: SearchModalProps) {
  const [query, setQuery] = useState("");
  const [searchedTerm, setSearchedTerm] = useState("");
  const [results, setResults] = useState<TMDBMovie[]>([]);
  const [hasSearched, setHasSearched] = useState(false);
  const [isSearching, setIsSearching] = useState(false);
  const [pendingId, setPendingId] = useState<number | null>(null);
  const inputRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    const t = window.setTimeout(() => inputRef.current?.focus(), 60);
    return () => window.clearTimeout(t);
  }, []);

  const handleSearch = async (e: FormEvent) => {
    e.preventDefault();
    const term = query.trim();
    if (!term || isSearching) return;
    setIsSearching(true);
    setHasSearched(true);
    setSearchedTerm(term);
    try {
      setResults(await APIClient.tmdb.search(term));
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Failed to search movies");
      setResults([]);
    } finally {
      setIsSearching(false);
    }
  };

  // A single shared add request is in flight at a time, scoped to one result
  // card by id; SSE refreshes the affected caches, so no mutation wrapper needed.
  const handleAdd = async (movie: TMDBMovie, close: () => void) => {
    if (pendingId !== null) return;
    setPendingId(movie.id);
    try {
      await APIClient.users.addMovie(userID, movie.title, movie.id);
      toast.success(`${movie.title} added to ${userName}'s stash`);
      close();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : `Failed to add ${movie.title}`);
    } finally {
      setPendingId(null);
    }
  };

  return (
    <Modal onClose={onClose}>
      {(close) => (
        <>
          <div className="modal__head">
            <div className="top">
              <div>
                <h3>Search &amp; add movies</h3>
                <p>Find a movie on TMDB, then add it to {userName}'s stash.</p>
              </div>
              <button type="button" className="iconbtn" onClick={close} aria-label="Close">
                <XIcon />
              </button>
            </div>
            <form className="modal__search" onSubmit={handleSearch}>
              <label className="field">
                <SearchIcon />
                <input
                  ref={inputRef}
                  value={query}
                  onChange={(e) => setQuery(e.target.value)}
                  placeholder="Search title (e.g. Dune, The Matrix)…"
                  disabled={isSearching}
                />
              </label>
              <button type="submit" className="btn btn--accent" disabled={!query.trim() || isSearching}>
                {isSearching ? <Loader2Icon className="animate-spin mg-spin" /> : <SearchIcon />}
                {isSearching ? "Searching…" : "Search"}
              </button>
            </form>
          </div>

          {hasSearched && (
            <div className="modal__results">
              <div className="modal__count">
                {isSearching
                  ? "Looking up matches…"
                  : `${results.length} result${results.length === 1 ? "" : "s"}${searchedTerm ? ` for "${searchedTerm}"` : ""}`}
              </div>

              {!isSearching && results.length === 0 ? (
                <p className="empty">No matches found. Try a broader title.</p>
              ) : (
                <div className="result-grid">
                  {results.map((movie) => {
                    const busy = pendingId === movie.id;
                    const year = yearOf(movie.release_date);
                    return (
                      <div className="result" key={movie.id}>
                        <div className="result__media">
                          <Poster title={movie.title} hue={hueOf(movie.title)} posterPath={movie.poster_path ?? undefined} />
                          <div className="r-overlay" data-busy={busy}>
                            <div>
                              <div className="r-overlay__title">{movie.title}</div>
                              <div className="r-overlay__year">{year ?? "—"}</div>
                              <p className="r-overlay__desc">{movie.overview || "No overview available."}</p>
                            </div>
                            <button
                              type="button"
                              className="btn btn--accent btn--sm"
                              onClick={() => handleAdd(movie, close)}
                              disabled={busy}
                            >
                              {busy ? <Loader2Icon className="animate-spin mg-spin" /> : <PlusIcon />}
                              {busy ? "Adding…" : "Add"}
                            </button>
                          </div>
                        </div>
                        <div className="r-info">
                          <span className="r-title">{movie.title}</span>
                          <span className="r-year">{year ?? "—"}</span>
                        </div>
                      </div>
                    );
                  })}
                </div>
              )}
            </div>
          )}
        </>
      )}
    </Modal>
  );
}
