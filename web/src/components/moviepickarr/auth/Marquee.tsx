import { useQuery } from "@tanstack/react-query";

import { PosterWallQueryOptions } from "@/api/queries";

import {
  posterWall,
  WALL_COLUMNS,
  WALL_ROWS,
} from "@/components/moviepickarr/auth/posterWall";
import { posterUrl } from "@/components/moviepickarr/lib";

/** The cinematic left panel shared by the login and claim screens. Decorative
 *  only (aria-hidden): a tilted, layered-depth wall of movie posters the form
 *  column does not depend on. Real posters come from the public poster-wall
 *  endpoint (popularity order, centre-fanned so #1 sits at the visual middle);
 *  the gradient stand-ins fill any empty slot and stand in for the whole wall
 *  whenever the fetch is empty, still loading, or errored, so it never breaks. */
export function Marquee() {
  const wall = useQuery(PosterWallQueryOptions());
  const tiles = posterWall(wall.data ?? []);

  return (
    <aside className="auth__stage" aria-hidden>
      <div className="auth__wall">
        {Array.from({ length: WALL_COLUMNS }, (_, ci) => (
          <div key={ci} className="auth__col">
            {tiles.slice(ci * WALL_ROWS, ci * WALL_ROWS + WALL_ROWS).map((tile, ti) => {
              const [a, b] = tile.hues.split(" ");
              const url = posterUrl(tile.path);
              return (
                <span
                  key={ti}
                  className="auth__tile"
                  style={{
                    background: `linear-gradient(150deg, oklch(0.4 0.09 ${a}), oklch(0.2 0.05 ${b}))`,
                  }}
                >
                  {url && (
                    <img
                      className="auth__poster"
                      src={url}
                      alt=""
                      loading="lazy"
                      decoding="async"
                      // Decorative wall, so no per-image loading state or the
                      // Poster crossfade: the gradient underlay already covers
                      // the pre-load frame. On a 404, drop the broken image so
                      // the underlay shows through instead of an empty box.
                      onError={(e) => {
                        e.currentTarget.style.display = "none";
                      }}
                    />
                  )}
                </span>
              );
            })}
          </div>
        ))}
      </div>
      <div className="auth__veil" />
    </aside>
  );
}
