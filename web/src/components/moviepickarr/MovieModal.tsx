import { useQuery } from "@tanstack/react-query";
import { ExternalLinkIcon, XIcon } from "lucide-react";
import { Fragment, useLayoutEffect, useRef, useState } from "react";

import { MovieDetailQueryOptions } from "@/api/queries";

import { Avatar, MetaChips } from "@/components/moviepickarr/Bits";
import { backdropBg, backdropUrl, externalLinks, fullDate, hueOf, posterUrl, profileUrl, tmdbPersonUrl } from "@/components/moviepickarr/lib";
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

/** One credit line's worth of held space (`height: 1lh`), carrying a shorter
 *  skeleton bar inside it — so the row a landing credit will fill is already
 *  the right height rather than the height of the bar. */
function GhostCreditRow({ w }: { w: number }) {
  return (
    <span className="moviemodal__credits__ghost" aria-hidden="true">
      <SkeletonText w={w} h={12} />
    </span>
  );
}

/** Modal hero backdrop — the wide-format twin of `Poster`. The procedural
 *  duotone (backdropBg) is painted underneath as the instant first frame, so a
 *  slow TMDB CDN fetch crossfades in over colour instead of flashing the surface
 *  through (pure white in light mode). Mirrors Poster's cache-sync + shimmer. */
function HeroBackdrop({ hue, src }: { hue: number; src: string | null }) {
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
    </div>
  );
}

/** Read-only detail view for a movie: backdrop, a rail of poster + links out,
 *  the credits with the attribution beside them, overview, and the cast strip. */
export function MovieModal({
  movie,
  open,
  onRequestClose,
  onClose,
}: {
  movie: Movie;
  /** False once the backing history entry is gone, which plays the exit (#196). */
  open: boolean;
  /** Every dismiss gesture goes here, so all four pop the same entry. */
  onRequestClose: () => void;
  onClose: () => void;
}) {
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
  const hasCredits = directors.length > 0 || writers.length > 0;
  // HeroBackdrop always paints the procedural duotone base; this is just the
  // photo that crossfades over it (real backdrop, else the poster as a stand-in).
  const heroSrc = m.backdropPath
    ? backdropUrl(m.backdropPath)
    : m.posterPath
      ? posterUrl(m.posterPath, "w500")
      : null;

  return (
    // Capped (#177): the surface caps at the window height and scrolls inside
    // itself, so a long record centers in the window instead of dragging the
    // blurred page with it, and the close X — pinned to the surface, outside
    // `.modal__scroll` — stays put while the hero scrolls under it.
    <Modal
      onClose={onClose}
      open={open}
      onRequestClose={onRequestClose}
      className="modal--movie"
      capped
    >
      {(close) => (
        <>
          <button type="button" className="iconbtn moviemodal__close" onClick={close} aria-label="Close">
            <XIcon />
          </button>

          <div className="modal__scroll">
            <HeroBackdrop hue={hue} src={heroSrc} />

            <div className="moviemodal__body">
              {/* The rail: identity, then the links out as reference material
                  attached to the film — quiet mono lines, not three buttons. */}
              <div className="moviemodal__rail">
                <Poster
                  title={m.title}
                  hue={hue}
                  posterPath={m.posterPath}
                  showTitle={!m.posterPath}
                />

                {links.length > 0 && (
                  <div className="moviemodal__links">
                    {links.map((link) => (
                      <a key={link.label} href={link.href} target="_blank" rel="noopener noreferrer">
                        <ExternalLinkIcon />
                        {link.label}
                      </a>
                    ))}
                  </div>
                )}
              </div>

              <div className="moviemodal__info">
                <h3>{m.title}</h3>
                {/* The chips navigate over the modal's own history entry, which
                    is what closes it: on /stats that's a same-route search
                    change, so the surface stays mounted and animates out over
                    the freshly-filtered view (see MetaChips). */}
                <MetaChips movie={m} replace />

                {/* "Directed by" and "Added by" are the same kind of line — who
                    is responsible for this — so they read as one block split by
                    a rule, instead of the attribution trailing the overview
                    where it belonged to nothing. */}
                <div className="moviemodal__credit">
                  {(hasCredits || detailLoading) && (
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
                      {/* Credits arrive with the lazy detail, so reserve the rows
                          still missing at full line height — the way the overview
                          and the cast strip already do — instead of letting the
                          block grow under the reader when they land. */}
                      {directors.length === 0 && <GhostCreditRow w={186} />}
                      {writers.length === 0 && <GhostCreditRow w={150} />}
                    </div>
                  )}

                  <div className="moviemodal__credits moviemodal__by">
                    <span>
                      Added by <b>{m.addedByName}</b>
                      {m.addedAt && ` · ${fullDate(m.addedAt)}`}
                    </span>
                    {m.watchedAt && <span>Watched {fullDate(m.watchedAt)}</span>}
                  </div>
                </div>

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
                {Array.from({ length: 9 }).map((_, i) => (
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
        </>
      )}
    </Modal>
  );
}
