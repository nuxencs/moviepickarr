/* Members page loading state (#239). Built from the page's own containers so
 * it inherits the layout, breakpoints and wall column count. Two rules: no
 * outline marks (an unfilled pip or dashed slot claims a value), only shimmer;
 * and row and tile counts are shape, not a guess at the roster, since each
 * column scrolls on its own and extra rows clip without moving the layout.
 */

import { POOL_SIZE } from "@/components/moviepickarr/poolLock";
import { Skeleton, SkeletonPoster } from "@/components/moviepickarr/Skeletons";

import "@/components/moviepickarr/members.css";

/** Fills the rail at desktop height. A shape, not a count: never wire it to the roster size. */
const RAIL_ROWS = 6;

/**
 * More tiles than any pane holds, clipped with `overflow: hidden` so the filler
 * is not scrollable. Below 761px the page scrolls the wall, so members.css caps
 * it with `nth-child` instead.
 */
const WALL_TILES = 126;

/** Varied so six bars read as people, not a table. The mono count lines stay uniform. */
const NAME_WIDTHS = [86, 104, 72, 96, 80, 110];

/** Not exported from Skeletons.tsx: a non-component export there trips react-refresh. */
const range = (n: number) => Array.from({ length: n });

/** Shimmer squares, not unfilled pips: those would claim an occupancy. */
function SkeletonPips() {
  return (
    <span className="mem-pips">
      {range(POOL_SIZE).map((_, i) => (
        <Skeleton key={i} w={7} h={7} className="mem-skel__pip" />
      ))}
    </span>
  );
}

/**
 * The shimmering rail and pane. Your own row is not drawn even though
 * `/auth/me` is cached: the rail does not mark self, and a non-401 session
 * failure falls through the auth guard. The page's `data-pushed` picks the
 * screen below 761px, so a deep link shimmers the screen it lands on.
 * Decorative: the page head's live region speaks for it.
 */
export function MembersSkeleton() {
  return (
    <div className="mem__shell mem-skel" aria-hidden="true">
      <div className="mem-rail-screen">
        <div className="mem-rail">
          {range(RAIL_ROWS).map((_, i) => (
            // No `data-active`: a gold accent would mark the wrong row on a deep link.
            <div className="mem-row" key={i}>
              <div className="mem-row__link">
                <Skeleton w={30} h={30} radius="sm" />
                <span className="mem-row__text">
                  <Skeleton w={NAME_WIDTHS[i % NAME_WIDTHS.length]} h={11} />
                  <Skeleton w={58} h={9} style={{ marginTop: 5 }} />
                </span>
                <SkeletonPips />
              </div>

              {/* Row 0 is always open: the real rail is N rows plus one open
                  drawer wherever it is, so the column height matches. */}
              {i === 0 && (
                <div className="mem-drop" data-open="true">
                  <div className="mem-drop__inner">
                    <div className="mem-drop__body">
                      <div className="mem-pool">
                        {range(POOL_SIZE).map((_, slot) => (
                          <SkeletonPoster key={slot} />
                        ))}
                      </div>
                      {/* Hidden above 760px; on the phone rail it is the way forward. */}
                      <Skeleton className="mem-skel__tostash" />
                    </div>
                  </div>
                </div>
              )}
            </div>
          ))}
        </div>
      </div>

      <div className="mem-pane">
        <div className="mem-backbar">
          <Skeleton w={104} h={15} />
          <SkeletonPips />
        </div>

        <div className="mem-stash">
          {/* The field is reserved even though the real one hides at zero
              movies: below 900px the head stacks, and its arrival would push
              the wall down. The count hides above 760px like the real one. */}
          <div className="mem-stash__head">
            <div className="mem-stash__id">
              <Skeleton w={132} h={15} />
              <Skeleton className="mem-skel__ct" />
            </div>
            <Skeleton className="mem-skel__field" />
          </div>

          <div className="mem-wallbox mem-skel__wall">
            {/* No `data-overflow`: a fade promises a scroller, and this one cannot scroll. */}
            <div className="mem-wall">
              {range(WALL_TILES).map((_, i) => (
                <SkeletonPoster key={i} />
              ))}
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}
