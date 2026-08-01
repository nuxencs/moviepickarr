/* What the Members page draws while the roster is in flight (#239).
 *
 * It is built out of the page's own containers — .mem__shell, .mem-rail,
 * .mem-drop, .mem-pane, .mem-stash__head, .mem-wallbox, .mem-wall — so it
 * inherits the layout, the breakpoint ladder and the wall's column count
 * instead of restating them somewhere they can drift. What members.css adds
 * for it is only the handful of blocks that have no real counterpart.
 *
 * Two rules run through all of it.
 *
 * Nothing on screen claims a value it does not have. That rules out the
 * design's own outline marks even where they look like placeholders: an
 * unfilled pip says "0 of 3 filled" and a dashed cell says "this pool is
 * empty", and the empty pool slot was given that meaning deliberately. They
 * shimmer instead, so the loading state is one treatment throughout.
 *
 * And it draws shape, never extent. The rail's six rows and the wall's tiles
 * are not guesses at a roster or a stash; they are how much of the column
 * shimmers. Above 760 that costs nothing either way: the shell is the viewport
 * minus chrome with stretched items, and the rail and the wall are each their
 * own scroller, so over-drawing clips and under-drawing leaves blank column.
 * Neither moves a pixel of layout.
 */

import { POOL_SIZE } from "@/components/moviepickarr/poolLock";
import { Skeleton, SkeletonPoster } from "@/components/moviepickarr/Skeletons";

import "@/components/moviepickarr/members.css";

/** Enough rows to fill the rail at a desktop height, and a shape rather than a
 *  count: nothing may wire this to a roster size. Over-drawing clips against
 *  the rail's own scroller and under-drawing leaves grey column, so the number
 *  is free either way. */
const RAIL_ROWS = 6;

/**
 * More tiles than any pane will hold, so the wall fills the space it is given
 * at every viewport height and every step of the root zoom ramp without
 * measuring one. The box clips the rest, with `overflow: hidden` rather than
 * `auto` — the overdrawn tail is filler, so it must not be reachable.
 *
 * Below 760 the wall stops being a scroller and the page carries it, so there
 * is no ceiling left to clip against and the tail would become page length
 * instead. The cap there is CSS (`nth-child`), not a width branch in JS, for
 * the same no-measurement reason.
 */
const WALL_TILES = 36;

/** The name lines differ in width, because six identical bars read as a table
 *  rather than as six people. The counts do not: they stand in for `N in
 *  stash` in mono, so a real rail's second lines are the same length give or
 *  take a digit. */
const NAME_WIDTHS = [86, 104, 72, 96, 80, 110];

/** Local rather than shared with Skeletons.tsx: exporting it from a .tsx trips
 *  react-refresh, and a one-line Array.from is not worth a new module. */
const range = (n: number) => Array.from({ length: n });

/** Three shimmer squares where the pips go. Not three unfilled pips: those are
 *  a statement of occupancy, and this does not know the occupancy. */
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
 * The rail beside the pane, both of them shimmering.
 *
 * Member-agnostic, your own row included, and that is a choice rather than a
 * constraint: the app layout gates on the session, so `/auth/me` is in cache
 * before this renders and your name and avatar are available. Drawing them in
 * row 0 puts two kinds of row on screen and re-marks self in the rail, which
 * the rail spent a decision removing — and abstract is the one shape that also
 * survives a non-401 failure of the session endpoint, which the auth guard
 * deliberately lets fall through.
 *
 * Which screen this is below 760 is not decided here either. The page's own
 * `data-pushed` is the URL in every state, pending included, so the existing
 * media query picks the screen and a cold deep link onto a member's stash
 * spends the flight on the screen it is arriving at rather than shimmering the
 * rail and then swapping.
 *
 * Purely decorative. What a screen reader hears during the flight is the live
 * region beside the page head, which is not hidden and is not part of this.
 */
export function MembersSkeleton() {
  return (
    <div className="mem__shell mem-skel" aria-hidden="true">
      <div className="mem-rail-screen">
        <div className="mem-rail">
          {range(RAIL_ROWS).map((_, i) => (
            // No `data-active` on any row, so no accent line. The open drawer is
            // already the mark, a gold line would be the only saturated thing on
            // a grey page, and on a deep link it would be marking the wrong row.
            <div className="mem-row" key={i}>
              <div className="mem-row__link">
                <Skeleton w={30} h={30} radius="sm" />
                <span className="mem-row__text">
                  <Skeleton w={NAME_WIDTHS[i % NAME_WIDTHS.length]} h={11} />
                  <Skeleton w={58} h={9} style={{ marginTop: 5 }} />
                </span>
                <SkeletonPips />
              </div>

              {/* Row 0 is open, always, whatever the URL names. There is no
                  better guess and the rail's height does not depend on it.
                  Every drawer stays mounted and shut at 0fr on the real rail,
                  so the rail is always N rows plus exactly one drawer's worth:
                  when the data lands the open one relocates, it does not grow
                  the column. */}
              {i === 0 && (
                <div className="mem-drop" data-open="true">
                  <div className="mem-drop__inner">
                    <div className="mem-drop__body">
                      <div className="mem-pool">
                        {range(POOL_SIZE).map((_, slot) => (
                          <SkeletonPoster key={slot} />
                        ))}
                      </div>
                      {/* Off-screen above 760, where the pane is already beside
                          the rail; on the phone rail it is the only way forward,
                          so it is reserved. */}
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
        {/* Same as the real one: drawn on the pushed screen and nowhere else. */}
        <div className="mem-backbar">
          <Skeleton w={104} h={15} />
          <SkeletonPips />
        </div>

        <div className="mem-stash">
          {/* The heading is the possessive one, which is the pane's
              load-bearing self-mark and unknowable here. One line stands in
              for it.

              The field gets a block even though the real one is suppressed at
              zero films: above 900 the head is a row, so reserving it costs no
              vertical shift, and below 900 the head is a column, where leaving
              it out would let the field's arrival push the whole wall down.

              The count block follows the real count's own rule — off above
              760, where the rail carries every member's number instead. */}
          <div className="mem-stash__head">
            <div className="mem-stash__id">
              <Skeleton w={132} h={15} />
              <Skeleton className="mem-skel__ct" />
            </div>
            <Skeleton className="mem-skel__field" />
          </div>

          <div className="mem-wallbox mem-skel__wall">
            {/* No `data-overflow`, so no fade: a fade promises a scroller, and
                this one cannot be scrolled. */}
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
