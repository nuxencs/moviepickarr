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

import type { MovieTile } from "@/types/Response";

/** Initials avatar; an optional `src` photo layers over it. */
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

  // Read a cached photo's `complete` before paint so it crossfades, not pops.
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
      {/* Every call site writes the name beside it; spoken, it reads "AD Ada". */}
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

export function AdderTag({ name, size = 20 }: { name: string; size?: number }) {
  return (
    <span className="flex min-w-0 items-center gap-2">
      <Avatar name={name} size={size} />
      <span className="truncate text-[13px] text-ink-2">{name}</span>
    </span>
  );
}

/** year · runtime · rating | genres | links; each piece omitted when absent. */
export function MetaChips({
  movie,
  links = [],
  replace = false,
}: {
  movie: MovieTile;
  links?: { label: string; href: string }[];
  /**
   * Set by the movie modal: the chip replaces the modal's history entry (see
   * useMovieModalHistory). A separate pop would race the chip's navigation.
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
