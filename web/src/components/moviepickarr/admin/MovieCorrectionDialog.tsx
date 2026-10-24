import { useMutation } from "@tanstack/react-query";
import { CalendarClockIcon, CheckIcon, Loader2Icon, SearchIcon, XIcon } from "lucide-react";
import { type FormEvent, useId, useMemo, useState } from "react";

import { APIClient, ApiError } from "@/api/APIClient";

import { toISODateTime, toLocalDateTimeInputValue } from "@/components/moviepickarr/admin/watchedDate";
import { hueOf, yearOf } from "@/components/moviepickarr/lib";
import { Modal } from "@/components/moviepickarr/Modal";
import { Poster } from "@/components/moviepickarr/Poster";
import { toast } from "@/components/ui/toast-api";

import type { LibraryMovie, MovieCorrection, TMDBMovie } from "@/types/Response";

function correctionErrorMessage(error: unknown): string {
  if (error instanceof ApiError) {
    switch (error.code) {
      case "acquisition_locked":
        return "Radarr already holds this movie. Abandon its acquisition first.";
      case "draw_in_progress":
        return "A draw is in progress. Correct the movie after the reveal.";
      case "conflict":
        return "Another movie in the library already uses this TMDB entry.";
    }
  }
  return "The correction failed. Try again.";
}

/** An admin Movie correction (ADR 0013): a new TMDB identity, a watched date, or both. */
export function MovieCorrectionDialog({
  movie,
  libraryTitles,
  onClose,
}: {
  movie: LibraryMovie;
  /** Library titles by TMDB id, so a pick that another movie owns is refused before save. */
  libraryTitles: ReadonlyMap<number, string>;
  onClose: () => void;
}) {
  const isWatched = movie.status === "watched";
  const initialWatchedAtLocal = useMemo(
    () => toLocalDateTimeInputValue(movie.watchedAt),
    [movie.watchedAt],
  );
  const [watchedAtLocal, setWatchedAtLocal] = useState(initialWatchedAtLocal);
  const [watchedAtTouched, setWatchedAtTouched] = useState(false);
  const [query, setQuery] = useState("");
  const [results, setResults] = useState<TMDBMovie[] | null>(null);
  const [searchFailed, setSearchFailed] = useState(false);
  const [isSearching, setIsSearching] = useState(false);
  const [picked, setPicked] = useState<TMDBMovie | null>(null);
  const resultsName = useId();
  const watchedAtErrorID = useId();
  const tmdbHeadingID = useId();
  const watchedHeadingID = useId();

  const correction = useMutation({
    mutationFn: (payload: MovieCorrection) =>
      APIClient.maintenance.correctMovie(movie.movieID, payload),
  });

  const watchedAtISO = toISODateTime(watchedAtLocal);
  let watchedAtError: string | undefined;
  if (isWatched && watchedAtTouched && !watchedAtISO) {
    watchedAtError = watchedAtLocal.trim()
      ? "Enter a valid watched date and time."
      : "Watched date and time is required.";
  }
  // An unchanged minute-level value is omitted, so the stored seconds survive.
  const watchedAtChanged = isWatched && watchedAtLocal !== initialWatchedAtLocal;
  const payload: MovieCorrection = {
    ...(picked ? { tmdbId: picked.id, title: picked.title, year: yearOf(picked.release_date) } : {}),
    ...(watchedAtChanged && watchedAtISO ? { watchedAt: watchedAtISO } : {}),
  };
  const canSave =
    !correction.isPending &&
    (!isWatched || watchedAtISO !== undefined) &&
    (payload.tmdbId !== undefined || payload.watchedAt !== undefined);

  const search = async (event: FormEvent) => {
    event.preventDefault();
    const term = query.trim();
    if (!term || isSearching) return;
    setIsSearching(true);
    setSearchFailed(false);
    try {
      setResults(await APIClient.tmdb.search(term));
    } catch {
      setSearchFailed(true);
      setResults([]);
    } finally {
      setIsSearching(false);
    }
  };

  const year = yearOf(movie.releaseDate);
  const facts = [movie.title, year, `added by ${movie.addedByName}`].filter(Boolean).join(" · ");

  return (
    <Modal
      label="Correct movie"
      className="modal--correction"
      capped
      dismissible={!correction.isPending}
      onClose={onClose}
    >
      {(close) => (
        <>
          <div className="modal__head">
            <div className="top">
              <div>
                <h3>Correct movie</h3>
                <p>{facts}</p>
              </div>
              <button
                type="button"
                className="iconbtn"
                onClick={close}
                aria-label="Close"
                disabled={correction.isPending}
              >
                <XIcon />
              </button>
            </div>
          </div>

          <div className="modal__scroll correction__scroll">
            <section className="correction__section correction__section--tmdb" aria-labelledby={tmdbHeadingID}>
              <div className="correction__section-head">
                <h4 id={tmdbHeadingID}>TMDB movie</h4>
                <span className="correction__current mono">
                  {movie.tmdbId ? `Now TMDB ${movie.tmdbId}` : "No TMDB id"}
                </span>
              </div>
              <form className="correction__search" onSubmit={search}>
                <label className="field">
                  <SearchIcon />
                  <input
                    type="search"
                    name="tmdb-search"
                    aria-label="TMDB title"
                    value={query}
                    onChange={(event) => setQuery(event.target.value)}
                    placeholder="Movie title"
                    disabled={correction.isPending}
                  />
                </label>
                <button
                  type="submit"
                  className="btn btn--ghost"
                  disabled={!query.trim() || isSearching || correction.isPending}
                >
                  {isSearching && <Loader2Icon className="animate-spin mg-spin" />}
                  Search TMDB
                </button>
              </form>

              {results !== null && !isSearching && results.length === 0 && (
                <p className="correction__empty">
                  {searchFailed ? "TMDB search failed. Try again." : "No matches. Try a broader title."}
                </p>
              )}
              {results !== null && results.length > 0 && (
                <div className="correction__results" role="radiogroup" aria-label="TMDB results">
                  {results.map((result) => {
                    const isCurrent = result.id === movie.tmdbId;
                    const takenBy = isCurrent ? undefined : libraryTitles.get(result.id);
                    const isPicked = picked?.id === result.id;
                    const resultYear = yearOf(result.release_date);
                    return (
                      <label
                        key={result.id}
                        className="correction-result"
                        data-unavailable={isCurrent || takenBy !== undefined || undefined}
                      >
                        <input
                          type="radio"
                          className="vis-hidden"
                          name={resultsName}
                          checked={isPicked}
                          disabled={isCurrent || takenBy !== undefined || correction.isPending}
                          onChange={() => setPicked(result)}
                        />
                        <Poster
                          title={result.title}
                          hue={hueOf(result.title)}
                          posterPath={result.poster_path ?? undefined}
                          showTitle={false}
                          sizes="auto, 44px"
                        />
                        <span className="correction-result__text">
                          <strong>{result.title}</strong>
                          <span className="mono">
                            {[
                              resultYear,
                              `TMDB ${result.id}`,
                              isCurrent ? "current" : undefined,
                              takenBy !== undefined ? `in library as ${takenBy}` : undefined,
                            ]
                              .filter(Boolean)
                              .join(" · ")}
                          </span>
                        </span>
                        <span className="correction-result__check" aria-hidden="true">
                          {isPicked && <CheckIcon />}
                        </span>
                      </label>
                    );
                  })}
                </div>
              )}
            </section>

            {isWatched && (
              <section className="correction__section" aria-labelledby={watchedHeadingID}>
                <div className="correction__section-head">
                  <h4 id={watchedHeadingID}>Watched date</h4>
                </div>
                <div className="fieldgroup">
                  <label className="field" data-invalid={watchedAtError ? true : undefined}>
                    <CalendarClockIcon />
                    <input
                      type="datetime-local"
                      name="watched-at"
                      aria-label="Watched date and time"
                      aria-required="true"
                      aria-invalid={watchedAtError ? true : undefined}
                      aria-describedby={watchedAtError ? watchedAtErrorID : undefined}
                      value={watchedAtLocal}
                      onChange={(event) => setWatchedAtLocal(event.target.value)}
                      onBlur={() => setWatchedAtTouched(true)}
                      required
                      disabled={correction.isPending}
                    />
                  </label>
                  {watchedAtError && (
                    <p id={watchedAtErrorID} className="field-error" role="alert">
                      {watchedAtError}
                    </p>
                  )}
                </div>
              </section>
            )}

          </div>

          <div className="modal__foot">
            <button type="button" className="btn btn--ghost" onClick={close} disabled={correction.isPending}>
              Cancel
            </button>
            <button
              type="button"
              className="btn btn--accent"
              disabled={!canSave}
              onClick={() =>
                correction.mutate(payload, {
                  onSuccess: () => {
                    toast.success(`${movie.title} corrected`);
                    close();
                  },
                  // A toast stays in view; an inline line sat under the scrolling results.
                  onError: (error) => toast.error(correctionErrorMessage(error)),
                })
              }
            >
              {correction.isPending && <Loader2Icon className="animate-spin mg-spin" />}
              {correction.isPending ? "Saving…" : "Save correction"}
            </button>
          </div>
        </>
      )}
    </Modal>
  );
}
