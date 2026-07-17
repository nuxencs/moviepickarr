import { useQuery } from "@tanstack/react-query";
import { ExternalLinkIcon, XIcon } from "lucide-react";
import { Fragment, type ReactNode, useLayoutEffect, useRef, useState } from "react";

import { MovieDetailQueryOptions } from "@/api/queries";

import { Avatar, MetaChips } from "@/components/moviepickarr/Bits";
import { backdropBg, backdropUrl, externalLinks, hueOf, posterUrl, profileUrl, tmdbPersonUrl } from "@/components/moviepickarr/lib";
import { Modal } from "@/components/moviepickarr/Modal";
import { Poster } from "@/components/moviepickarr/Poster";
import { SkeletonText } from "@/components/moviepickarr/Skeletons";

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

/** Modal hero backdrop — the wide-format twin of `Poster`. The procedural
 *  duotone (backdropBg) is painted underneath as the instant first frame, so a
 *  slow TMDB CDN fetch crossfades in over colour instead of flashing the surface
 *  through (pure white in light mode). Mirrors Poster's cache-sync + shimmer. */
function HeroBackdrop({ hue, src, children }: { hue: number; src: string | null; children?: ReactNode }) {
  const [loaded, setLoaded] = useState(false);
  const [failed, setFailed] = useState(false);
  const imgRef = useRef<HTMLImageElement>(null);

  // Sync `loaded` from a cached image's `complete` before paint so a reopened
  // modal (or an SSE-swapped backdrop) doesn't re-flash the placeholder.
  useLayoutEffect(() => {
    const img = imgRef.current;
    setFailed(false);
    setLoaded(Boolean(img?.complete && img.naturalWidth > 0));
  }, [src]);

  const url = failed ? null : src;
  const loading = url !== null && !loaded;

  return (
    <div
      className={`moviemodal__hero${loading ? " moviemodal__hero--loading" : ""}`}
      style={{ backgroundImage: backdropBg(hue) }}
    >
      {url && (
        <img
          ref={imgRef}
          className="moviemodal__hero__img"
          src={url}
          alt=""
          onLoad={() => setLoaded(true)}
          onError={() => setFailed(true)}
        />
      )}
      {loading && <div className="moviemodal__hero__shimmer" aria-hidden="true" />}
      {children}
    </div>
  );
}

/** Rich detail view for a movie: backdrop, poster, metadata, credits, overview, links out. */
export function MovieModal({ movie, onClose }: { movie: Movie; onClose: () => void }) {
  // The list payloads are lean (no cast/crew/overview/backdrop), so lazy-load the
  // full record on open. `movie` (the tile's lean object) renders instantly while
  // the detail loads, then the enriched fields fill in. SSE enrichment events
  // invalidate this query, so an open modal updates live too.
  const { data: detail, isPending } = useQuery(MovieDetailQueryOptions(movie.movieID));
  const m = detail ?? movie;
  // Heavy fields (overview/credits/cast) live only in the detail payload; while
  // it loads, the lean tile object lacks them — show skeletons in their place so
  // the body fills in progressively instead of popping in all at once. A field
  // that's genuinely empty (query settled, not pending) renders nothing rather
  // than a perma-skeleton; a full list object (pool) shows its data immediately.
  const detailLoading = isPending;

  const hue = hueOf(m.title);
  const links = externalLinks(m);
  const cast = m.cast ?? [];
  const crew = m.crew ?? [];
  const directors = dedupeById(crew.filter((p) => p.job === "Director"));
  const writers = dedupeById(crew.filter((p) => p.job === "Writer" || p.job === "Screenplay"));
  // HeroBackdrop always paints the procedural duotone base; this is just the
  // photo that crossfades over it (real backdrop, else the poster as a stand-in).
  const heroSrc = m.backdropPath
    ? backdropUrl(m.backdropPath)
    : m.posterPath
      ? posterUrl(m.posterPath, "w500")
      : null;

  const watchedLabel = m.watchedAt
    ? `watched ${new Date(m.watchedAt).toLocaleDateString(undefined, { day: "numeric", month: "short", year: "numeric" })}`
    : null;

  return (
    <Modal onClose={onClose} className="modal--movie">
      {(close) => (
        <div className="moviemodal">
          <HeroBackdrop hue={hue} src={heroSrc}>
            <button type="button" className="iconbtn moviemodal__close" onClick={close} aria-label="Close">
              <XIcon />
            </button>
          </HeroBackdrop>

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

              {directors.length > 0 || writers.length > 0 ? (
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
              ) : detailLoading ? (
                <div className="moviemodal__credits" aria-hidden="true">
                  <SkeletonText w={186} h={12} />
                  <SkeletonText w={150} h={12} />
                </div>
              ) : null}

              {m.tagline && <p className="moviemodal__tag">"{m.tagline}"</p>}
              {m.overview ? (
                <p className="moviemodal__overview">{m.overview}</p>
              ) : detailLoading ? (
                <div className="moviemodal__overview" aria-hidden="true">
                  <SkeletonText w="100%" />
                  <SkeletonText w="100%" style={{ marginTop: 7 }} />
                  <SkeletonText w="62%" style={{ marginTop: 7 }} />
                </div>
              ) : null}

              <div className="moviemodal__by">
                added by {m.addedByName}
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

          {cast.length > 0 ? (
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
          ) : detailLoading ? (
            <div className="castrow" aria-hidden="true">
              {Array.from({ length: 8 }).map((_, i) => (
                <div className="castcard" key={i}>
                  {/* The 2:3 frame already; `skel` just adds the shimmer sweep. */}
                  <div className="castcard__photo skel" />
                  <span className="castcard__caption">
                    <SkeletonText w="80%" h={11} />
                    <SkeletonText w="55%" h={11} />
                  </span>
                </div>
              ))}
            </div>
          ) : null}
        </div>
      )}
    </Modal>
  );
}
