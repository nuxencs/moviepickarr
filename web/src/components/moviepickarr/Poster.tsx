import { memo, useLayoutEffect, useRef, useState } from "react";

import { posterBg, posterSrcSet, posterUrl } from "@/components/moviepickarr/lib";

export const GENERAL_POSTER_SIZES =
  "auto, (max-width: 640px) 104px, (max-width: 1199px) 128px, " +
  "clamp(144px, 6.4vw, 164px)";

interface PosterProps {
  title: string;
  hue: number;
  posterPath?: string;
  /** Show the procedural alt-poster title overlay (hidden in tight rows/slots). */
  showTitle?: boolean;
  /** Render-width hint that opts this poster into compact responsive sources. */
  sizes?: string;
  className?: string;
}

/**
 * The only real "container" in the design. Renders the TMDB poster when
 * available, otherwise a deterministic procedural duotone with the title
 * baked in as an alt-poster overlay. The CSS supplies the sheen + grain.
 */
// Memoized: all props are primitives, so identical tiles skip re-rendering when
// a parent re-renders for an unrelated reason (e.g. typing in the watched-grid
// search box re-runs the list map but the surviving tiles' Poster props are
// unchanged).
export const Poster = memo(function Poster({
  title,
  hue,
  posterPath,
  showTitle = true,
  sizes,
  className,
}: PosterProps) {
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
          srcSet={sizes ? posterSrcSet(posterPath) ?? undefined : undefined}
          sizes={sizes}
          alt={title}
          loading="lazy"
          onLoad={() => setLoaded(true)}
          onError={() => setImgFailed(true)}
        />
      )}

      {loading && <div className="poster__shimmer" aria-hidden="true" />}

      {/* The alt-poster title only makes sense on the procedural art (no photo). */}
      {showTitle && !url && (
        <div className="poster__title">
          <div className="poster__rule" />
          {title}
        </div>
      )}
    </div>
  );
});
