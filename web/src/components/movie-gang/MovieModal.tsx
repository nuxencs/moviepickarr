import { ExternalLinkIcon, XIcon } from "lucide-react";
import { Fragment } from "react";

import { Avatar, MetaChips } from "@/components/movie-gang/Bits";
import { backdropBg, backdropUrl, externalLinks, hueOf, posterUrl, profileUrl, tmdbPersonUrl } from "@/components/movie-gang/lib";
import { Modal } from "@/components/movie-gang/Modal";
import { Poster } from "@/components/movie-gang/Poster";

import type { CreditPerson, Movie } from "@/types/Response";

/** First occurrence per person id (a writer credited for Writer AND Screenplay shows once). */
function dedupeById(people: CreditPerson[]): CreditPerson[] {
  const seen = new Set<number>();
  return people.filter((p) => {
    if (seen.has(p.id)) return false;
    seen.add(p.id);
    return true;
  });
}

/** Comma-separated credit names, each a link out to its TMDB person page. */
function PersonLinks({ people }: { people: CreditPerson[] }) {
  return (
    <>
      {people.map((p, i) => (
        <Fragment key={p.id}>
          {i > 0 && ", "}
          <a
            className="moviemodal__person"
            href={tmdbPersonUrl(p.id)}
            target="_blank"
            rel="noopener noreferrer"
          >
            {p.name}
          </a>
        </Fragment>
      ))}
    </>
  );
}

/** Rich detail view for a movie: backdrop, poster, metadata, credits, overview, links out. */
export function MovieModal({ movie, onClose }: { movie: Movie; onClose: () => void }) {
  const hue = hueOf(movie.title);
  const links = externalLinks(movie);
  const cast = movie.cast ?? [];
  const crew = movie.crew ?? [];
  const directors = dedupeById(crew.filter((p) => p.job === "Director"));
  const writers = dedupeById(crew.filter((p) => p.job === "Writer" || p.job === "Screenplay"));
  const heroBg = movie.backdropPath
    ? `url(${backdropUrl(movie.backdropPath)})`
    : movie.posterPath
      ? `url(${posterUrl(movie.posterPath, "w500")})`
      : backdropBg(hue);

  const watchedLabel = movie.watchedAt
    ? `watched ${new Date(movie.watchedAt).toLocaleDateString(undefined, { day: "numeric", month: "short", year: "numeric" })}`
    : null;

  return (
    <Modal onClose={onClose} className="modal--movie">
      {(close) => (
        <div className="moviemodal">
          <div className="moviemodal__hero" style={{ backgroundImage: heroBg }}>
            <button type="button" className="iconbtn moviemodal__close" onClick={close} aria-label="Close">
              <XIcon />
            </button>
          </div>

          <div className="moviemodal__body">
            <div className="moviemodal__poster">
              <Poster
                title={movie.title}
                hue={hue}
                posterPath={movie.posterPath}
                showTitle={!movie.posterPath}
              />
            </div>

            <div className="moviemodal__info">
              <h3>{movie.title}</h3>
              <MetaChips movie={movie} />

              {(directors.length > 0 || writers.length > 0) && (
                <div className="moviemodal__credits">
                  {directors.length > 0 && (
                    <span>
                      Directed by <PersonLinks people={directors} />
                    </span>
                  )}
                  {writers.length > 0 && (
                    <span>
                      Written by <PersonLinks people={writers} />
                    </span>
                  )}
                </div>
              )}

              {movie.tagline && <p className="moviemodal__tag">"{movie.tagline}"</p>}
              {movie.overview && <p className="moviemodal__overview">{movie.overview}</p>}

              <div className="moviemodal__by">
                picked by {movie.addedByName}
                {watchedLabel ? ` · ${watchedLabel}` : ""}
              </div>

              {links.length > 0 && (
                <div className="moviemodal__links">
                  {links.map((link, i) => (
                    <a
                      key={link.label}
                      className={`btn btn--sm ${i === 0 ? "btn--accent" : "btn--ghost"}`}
                      href={link.href}
                      target="_blank"
                      rel="noopener noreferrer"
                    >
                      <ExternalLinkIcon />
                      {link.label}
                    </a>
                  ))}
                </div>
              )}
            </div>
          </div>

          {cast.length > 0 && (
            <div className="castrow">
              {cast.map((p) => (
                <a
                  className="castcard"
                  key={p.id}
                  href={tmdbPersonUrl(p.id)}
                  target="_blank"
                  rel="noopener noreferrer"
                >
                  <div className="castcard__photo">
                    {/* Avatar carries the photo so a dead profile_path falls
                        back to the initials instead of a broken image. */}
                    <Avatar name={p.name} src={profileUrl(p.profilePath)} />
                  </div>
                  <span className="castcard__caption">
                    <span className="castcard__name">{p.name}</span>
                    {p.character && <span className="castcard__role">{p.character}</span>}
                  </span>
                </a>
              ))}
            </div>
          )}
        </div>
      )}
    </Modal>
  );
}
