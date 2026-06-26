import { useQuery } from "@tanstack/react-query";
import { ExternalLinkIcon, XIcon } from "lucide-react";
import { Fragment } from "react";

import { MovieDetailQueryOptions } from "@/api/queries";

import { Avatar, MetaChips } from "@/components/moviepickarr/Bits";
import { backdropBg, backdropUrl, externalLinks, hueOf, posterUrl, profileUrl, tmdbPersonUrl } from "@/components/moviepickarr/lib";
import { Modal } from "@/components/moviepickarr/Modal";
import { Poster } from "@/components/moviepickarr/Poster";

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
  // The list payloads are lean (no cast/crew/overview/backdrop), so lazy-load the
  // full record on open. `movie` (the tile's lean object) renders instantly while
  // the detail loads, then the enriched fields fill in. SSE enrichment events
  // invalidate this query, so an open modal updates live too.
  const { data: detail } = useQuery(MovieDetailQueryOptions(movie.movieID));
  const m = detail ?? movie;

  const hue = hueOf(m.title);
  const links = externalLinks(m);
  const cast = m.cast ?? [];
  const crew = m.crew ?? [];
  const directors = dedupeById(crew.filter((p) => p.job === "Director"));
  const writers = dedupeById(crew.filter((p) => p.job === "Writer" || p.job === "Screenplay"));
  const heroBg = m.backdropPath
    ? `url(${backdropUrl(m.backdropPath)})`
    : m.posterPath
      ? `url(${posterUrl(m.posterPath, "w500")})`
      : backdropBg(hue);

  const watchedLabel = m.watchedAt
    ? `watched ${new Date(m.watchedAt).toLocaleDateString(undefined, { day: "numeric", month: "short", year: "numeric" })}`
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
                title={m.title}
                hue={hue}
                posterPath={m.posterPath}
                showTitle={!m.posterPath}
              />
            </div>

            <div className="moviemodal__info">
              <h3>{m.title}</h3>
              {/* Pass `close` so a genre/year chip plays the modal's exit
                  animation before its /stats navigation (see MetaChips). */}
              <MetaChips movie={m} onNavigate={close} />

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

              {m.tagline && <p className="moviemodal__tag">"{m.tagline}"</p>}
              {m.overview && <p className="moviemodal__overview">{m.overview}</p>}

              <div className="moviemodal__by">
                picked by {m.addedByName}
                {watchedLabel ? ` · ${watchedLabel}` : ""}
              </div>

              {links.length > 0 && (
                <div className="moviemodal__links">
                  {links.map((link) => (
                    <a
                      key={link.label}
                      className="btn btn--sm btn--ghost"
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
