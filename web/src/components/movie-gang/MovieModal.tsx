import { ExternalLinkIcon, XIcon } from "lucide-react";

import { MetaChips } from "@/components/movie-gang/Bits";
import { backdropBg, backdropUrl, externalLinks, hueOf, posterUrl } from "@/components/movie-gang/lib";
import { Modal } from "@/components/movie-gang/Modal";
import { Poster } from "@/components/movie-gang/Poster";

import type { Movie } from "@/types/Response";

/** Rich detail view for a movie: backdrop, poster, metadata, overview, links out. */
export function MovieModal({ movie, onClose }: { movie: Movie; onClose: () => void }) {
  const hue = hueOf(movie.title);
  const links = externalLinks(movie);
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
        </div>
      )}
    </Modal>
  );
}
