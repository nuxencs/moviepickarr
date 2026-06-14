import { StarIcon } from "lucide-react";
import { useLayoutEffect, useRef, useState } from "react";

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
  const [loaded, setLoaded] = useState(false);
  const imgRef = useRef<HTMLImageElement>(null);

  // Instances are keyed by movie id, but a poster path can still arrive/retry
  // under SSE enrichment — reset on source change. Sync `loaded` from a cached
  // image's `complete` before paint so an already-loaded photo doesn't flash the
  // placeholder.
  useLayoutEffect(() => {
    const img = imgRef.current;
    setImgFailed(false);
    setLoaded(Boolean(img?.complete && img.naturalWidth > 0));
  }, [posterPath]);

  const url = imgFailed ? null : posterUrl(posterPath);
  const rating = ratingLabel(voteAverage);
  // The procedural duotone is ALWAYS the backdrop: the loading placeholder until
  // the photo fades in over it, and the permanent art when there's no/failed
  // image. `loading` drives the shimmer + the image's opacity fade.
  const loading = url !== null && !loaded;

  return (
    <div
      className={`poster${loading ? " poster--loading" : ""}${className ? ` ${className}` : ""}`}
      style={{ backgroundImage: posterBg(hue) }}
    >
      {url && (
        <img
          ref={imgRef}
          className="poster__img"
          src={url}
          alt={title}
          loading="lazy"
          onLoad={() => setLoaded(true)}
          onError={() => setImgFailed(true)}
        />
      )}

      {loading && <div className="poster__shimmer" aria-hidden="true" />}

      {rating && (
        <div className="poster__badge">
          <StarIcon />
          {rating}
        </div>
      )}

      {/* The alt-poster title only makes sense on the procedural art (no photo). */}
      {showTitle && !url && (
        <div className="poster__title">
          <div className="poster__rule" />
          {title}
        </div>
      )}
    </div>
  );
}
