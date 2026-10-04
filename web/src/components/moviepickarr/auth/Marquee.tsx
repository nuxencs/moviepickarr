import { useQuery } from "@tanstack/react-query";

import { PosterWallQueryOptions } from "@/api/queries";

import {
  posterWall,
  WALL_COLUMNS,
  WALL_ROWS,
} from "@/components/moviepickarr/auth/posterWall";
import { posterUrl } from "@/components/moviepickarr/lib";

/** Decorative poster wall for the login and claim screens. */
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
                      // No Poster crossfade: the gradient underlay covers the
                      // pre-load frame and shows through on a 404.
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
