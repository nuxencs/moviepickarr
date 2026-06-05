import { StarIcon } from "lucide-react";
import { useState } from "react";

import { posterBg, posterUrl, ratingLabel } from "@/components/movie-gang/lib";

interface PosterProps {
  title: string;
  hue: number;
  posterPath?: string;
  /** Show the procedural alt-poster title overlay (hidden in tight rows/slots). */
  showTitle?: boolean;
  /** TMDB rating to render as a top-left badge. */
  voteAverage?: number;
  className?: string;
}

/**
 * The only real "container" in the design. Renders the TMDB poster when
 * available, otherwise a deterministic procedural duotone with the title
 * baked in as an alt-poster overlay. The CSS supplies the sheen + grain.
 */
export function Poster({ title, hue, posterPath, showTitle = true, voteAverage, className }: PosterProps) {
  const [imgFailed, setImgFailed] = useState(false);
  const url = imgFailed ? null : posterUrl(posterPath);
  const rating = ratingLabel(voteAverage);

  return (
    <div
      className={`poster${className ? ` ${className}` : ""}`}
      style={url ? undefined : { backgroundImage: posterBg(hue) }}
    >
      {url && (
        <img
          className="poster__img"
          src={url}
          alt={title}
          loading="lazy"
          onError={() => setImgFailed(true)}
        />
      )}

      {rating && (
        <div className="poster__badge">
          <StarIcon />
          {rating}
        </div>
      )}

      {/* The alt-poster title only makes sense on the procedural art. */}
      {showTitle && !url && (
        <div className="poster__title">
          <div className="poster__rule" />
          {title}
        </div>
      )}
    </div>
  );
}
