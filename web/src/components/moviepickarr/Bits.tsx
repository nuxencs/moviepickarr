import { Link } from "@tanstack/react-router";
import { StarIcon } from "lucide-react";
import { useLayoutEffect, useRef, useState } from "react";

import {
  avatarBg,
  hueOf,
  initialsOf,
  ratingLabel,
  runtimeLabel,
  yearOf,
} from "@/components/moviepickarr/lib";
import { statsSearchDefaults } from "@/components/moviepickarr/statsSearch";

import type { Movie } from "@/types/Response";

/**
 * Square, hue-derived initials avatar. Hue defaults to a hash of the name.
 * An optional `src` photo (TMDB headshot) layers over the initials gradient,
 * falling back to the initials when missing or failing to load — the size
 * contract is identical either way.
 */
export function Avatar({
  name,
  size = 28,
  hue,
  src,
}: {
  name: string;
  size?: number;
  hue?: number;
  src?: string | null;
}) {
  const h = hue ?? hueOf(name);
  const [failedSrc, setFailedSrc] = useState<string | null>(null);
  const [loaded, setLoaded] = useState(false);
  const imgRef = useRef<HTMLImageElement>(null);
  const showImg = Boolean(src) && src !== failedSrc;

  // Sync `loaded` from a cached headshot's `complete` before paint, and reset on
  // a source change, so the photo crossfades in over the initials instead of
  // popping. Members without a photo never enter the loading state.
  useLayoutEffect(() => {
    const img = imgRef.current;
    setLoaded(Boolean(img?.complete && img.naturalWidth > 0));
  }, [src]);
  const loading = showImg && !loaded;

  return (
    <span
      className={`avatar${loading ? " avatar--loading" : ""}`}
      style={{ ["--s" as string]: `${size}px`, backgroundImage: avatarBg(h) }}
    >
      {/* The initials are art, not text: they are a fallback for a photo that
          is itself decorative (alt=""), and every call site writes the name
          next to them. Spoken, they turn each one into "AD Ada". */}
      <span aria-hidden="true">{initialsOf(name)}</span>
      {showImg && (
        <img
          ref={imgRef}
          className="avatar__img"
          src={src ?? undefined}
          alt=""
          loading="lazy"
          onLoad={() => setLoaded(true)}
          onError={() => setFailedSrc(src ?? null)}
        />
      )}
      {loading && <span className="avatar__shimmer" aria-hidden="true" />}
    </span>
  );
}

/** Mono rating with a star; dimmed star for low scores. Renders null when unrated. */
export function Rating({ voteAverage }: { voteAverage?: number }) {
  const label = ratingLabel(voteAverage);
  if (!label) return null;
  const low = (voteAverage ?? 0) < 6;
  return (
    <span className={`rating${low ? " rating--low" : ""}`}>
      <StarIcon />
      {label}
    </span>
  );
}

/** A small avatar + name tag used as a tile sublabel. */
export function AdderTag({ name, size = 20 }: { name: string; size?: number }) {
  return (
    <span className="flex min-w-0 items-center gap-2">
      <Avatar name={name} size={size} />
      <span className="truncate text-[13px] text-ink-2">{name}</span>
    </span>
  );
}

/**
 * year · runtime · rating | genre chips | external links, in mono. Each piece
 * is omitted when the underlying metadata is absent. A vertical rule divides
 * the rating facts from the genres, and the genres from the optional links.
 * `links` is opt-in (the hero passes them; the modal renders its own block).
 */
export function MetaChips({
  movie,
  links = [],
  replace = false,
}: {
  movie: Movie;
  links?: { label: string; href: string }[];
  /**
   * Navigate over the current history entry instead of stacking on it. The
   * movie modal sets it because its own entry is what the chips are leaving
   * (see useMovieModalHistory): the navigation consumes that entry, which
   * both closes the modal and keeps the stack flat. Popping it separately
   * would race the chip's own navigation, and on /stats (a same-route search
   * change that keeps the modal mounted) the pop could land back on it.
   */
  replace?: boolean;
}) {
  const year = yearOf(movie.releaseDate);
  const runtime = runtimeLabel(movie.runtime);
  const rating = ratingLabel(movie.voteAverage);
  const genres = (movie.genres ?? []).slice(0, 3);

  const hasFacts = Boolean(year || runtime || rating);
  const hasAny = hasFacts || genres.length > 0 || links.length > 0;
  if (!hasAny) return null;

  let dotted = false;
  const dot = () => {
    const cls = dotted ? "metachip metachip--dot" : "metachip";
    dotted = true;
    return cls;
  };

  return (
    <div className="metachips">
      {year && (
        // Release year deep-links to stats filtered to that year, mirroring the
        // genre chips below. Runtime/rating stay static — they aren't filters.
        <Link
          to="/stats"
          search={{ ...statsSearchDefaults, year }}
          className={`${dot()} metachip--link`}
          title={`See ${year} stats`}
          replace={replace}
        >
          {year}
        </Link>
      )}
      {runtime && <span className={dot()}>{runtime}</span>}
      {rating && (
        <span className={dot()}>
          <Rating voteAverage={movie.voteAverage} />
        </span>
      )}

      {genres.length > 0 && hasFacts && <span className="metasep" aria-hidden="true" />}
      {genres.map((g) => (
        // Deep-link to the Stats tab pre-filtered by this genre. stripSearchParams
        // trims the spread defaults, so the URL is just /stats?genre=<g>.
        <Link
          key={g}
          to="/stats"
          search={{ ...statsSearchDefaults, genre: g }}
          className="genrechip genrechip--link"
          title={`See ${g} stats`}
          replace={replace}
        >
          {g}
        </Link>
      ))}

      {links.length > 0 && (hasFacts || genres.length > 0) && (
        <span className="metasep" aria-hidden="true" />
      )}
      {links.map((link) => (
        <a
          key={link.label}
          className="metalink"
          href={link.href}
          target="_blank"
          rel="noopener noreferrer"
        >
          {link.label}
        </a>
      ))}
    </div>
  );
}
