import type { MovieTile } from "@/types/Response";

/**
 * The find-a-film path on an untitled wall, and the arithmetic behind moving
 * around it with the keyboard.
 *
 * The wall drops the caption under the tile, so the field is the only way to
 * get from a title you can name to the poster that carries it — and the only
 * text the pane still has to compose. Both rules live here, away from the
 * markup, because both are about the term rather than about the layout.
 *
 * The keyboard rules join them for the same reason: which cell an arrow key
 * reaches is index arithmetic over a cell count and a column count, and it can
 * be stated and tested without a wall to render (#235).
 */

/** How much of the term the miss line echoes. It is raw user input. */
const TERM_CAP = 32;

/** The stash narrowed to what the term matches, by title, anywhere, any case. */
export function filterStash(stash: MovieTile[], filter: string): MovieTile[] {
  const q = filter.trim().toLowerCase();
  // The same array back when there is no term: the wall is memoized per tile,
  // and a fresh array on every keystroke would be a new list identity for a
  // list that did not change.
  if (!q) return stash;
  return stash.filter((movie) => movie.title.toLowerCase().includes(q));
}

/**
 * What a wall with no hits says. Identical on your own board and a guest's:
 * the term missed, and whose stash it missed in is the heading's job.
 */
export function missLine(filter: string): string {
  const term = filter.trim();
  const shown = term.length > TERM_CAP ? `${term.slice(0, TERM_CAP)}…` : term;
  return `Nothing matches "${shown}"`;
}

/**
 * Which cell a key reaches from the one holding focus, or null for a key the
 * wall does not answer and for a move that runs off the end.
 *
 * Left and right step through the wall in its own order, which is the reading
 * order the columns lay out, so at the end of a row they carry on onto the next
 * one: the wall is one A-Z run of films and the responsive column count is the
 * stylesheet's business, which is the same reason it is not a `role="grid"`.
 * Up and down step by a row.
 *
 * A move with no cell at the other end is refused rather than clamped. Off the
 * first or last cell there is nowhere to go at all; off the last row, over a row
 * that is short, a Down that landed on the last film would move by a distance
 * nobody asked for and sideways as well as down.
 */
export function nextCell(key: string, from: number, cells: number, columns: number): number | null {
  if (cells <= 0) return null;
  const last = cells - 1;
  const within = (index: number) => (index < 0 || index > last ? null : index);
  switch (key) {
    case "ArrowRight":
      return within(from + 1);
    case "ArrowLeft":
      return within(from - 1);
    case "ArrowDown":
      return within(from + columns);
    case "ArrowUp":
      return within(from - columns);
    case "Home":
      return 0;
    case "End":
      return last;
    default:
      return null;
  }
}

/**
 * Where focus goes after the film at `vacated` leaves a band that has `left`
 * cells now, or null when nothing is left to hold it.
 *
 * The same rule on both bands: the cell that slides into the vacated index, and
 * the one before it when the vacated index was the end. What a null means is
 * the band's own — the pane heading for the wall, the member's row for a pool
 * emptied of films.
 */
export function landingCell(vacated: number, left: number): number | null {
  if (left <= 0) return null;
  return Math.min(Math.max(vacated, 0), left - 1);
}

/**
 * How many columns a computed `grid-template-columns` describes.
 *
 * The wall's column count is a container-query artifact of the pane's width, so
 * it is read from the resolved track list rather than computed a second time in
 * JavaScript. One column is the floor: a wall that
 * cannot be measured still moves up and down, a film at a time.
 */
export function columnCount(template: string): number {
  const tracks = template.trim();
  if (!tracks || tracks === "none") return 1;
  return tracks.split(/\s+/).length;
}
