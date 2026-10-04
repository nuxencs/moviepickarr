import { memo, useLayoutEffect, useRef, useState } from "react";

import { posterBg, posterSrcSet, posterUrl } from "@/components/moviepickarr/lib";

export const GENERAL_POSTER_SIZES =
  "auto, (max-width: 640px) 104px, (max-width: 1199px) 128px, " +
  "clamp(144px, 6.4vw, 164px)";

interface PosterProps {
  title: string;
  hue: number;
  posterPath?: string;
  /** Title overlay on the procedural art. */
  showTitle?: boolean;
  /** Render-width hint that opts this poster into compact responsive sources. */
  sizes?: string;
  className?: string;
}

// Memoized: all props are primitives, so unchanged tiles skip parent re-renders.
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

  // SSE enrichment can change the path under the same key. Read a cached image's
  // `complete` before paint so a loaded photo does not flash the placeholder.
  useLayoutEffect(() => {
    const img = imgRef.current;
    setImgFailed(false);
    setLoaded(Boolean(img?.complete && img.naturalWidth > 0));
  }, [posterPath]);

  const url = imgFailed ? null : posterUrl(posterPath);
  // The duotone is always painted: placeholder while loading, art without a photo.
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

      {showTitle && !url && (
        <div className="poster__title">
          <div className="poster__rule" />
          {title}
        </div>
      )}
    </div>
  );
});
