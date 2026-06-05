import { StarIcon } from "lucide-react";

import {
  avatarBg,
  hueOf,
  initialsOf,
  ratingLabel,
  runtimeLabel,
  yearOf,
} from "@/components/movie-gang/lib";

import type { Movie } from "@/types/Response";

/** Square, hue-derived initials avatar. Hue defaults to a hash of the name. */
export function Avatar({ name, size = 28, hue }: { name: string; size?: number; hue?: number }) {
  const h = hue ?? hueOf(name);
  return (
    <span className="avatar" style={{ ["--s" as string]: `${size}px`, backgroundImage: avatarBg(h) }}>
      {initialsOf(name)}
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
export function PickerTag({ name, size = 20 }: { name: string; size?: number }) {
  return (
    <span className="flex min-w-0 items-center gap-2">
      <Avatar name={name} size={size} />
      <span className="truncate text-[13px] text-ink-2">{name}</span>
    </span>
  );
}

/**
 * year · runtime · rating · up-to-3 genre chips, in mono. Each piece is
 * omitted when the underlying metadata is absent.
 */
export function MetaChips({ movie }: { movie: Movie }) {
  const year = yearOf(movie.releaseDate);
  const runtime = runtimeLabel(movie.runtime);
  const rating = ratingLabel(movie.voteAverage);
  const genres = (movie.genres ?? []).slice(0, 3);

  const hasAny = year || runtime || rating || genres.length > 0;
  if (!hasAny) return null;

  let dotted = false;
  const dot = () => {
    const cls = dotted ? "metachip metachip--dot" : "metachip";
    dotted = true;
    return cls;
  };

  return (
    <div className="metachips">
      {year && <span className={dot()}>{year}</span>}
      {runtime && <span className={dot()}>{runtime}</span>}
      {rating && (
        <span className={dot()}>
          <Rating voteAverage={movie.voteAverage} />
        </span>
      )}
      {genres.map((g) => (
        <span key={g} className="genrechip">
          {g}
        </span>
      ))}
    </div>
  );
}
