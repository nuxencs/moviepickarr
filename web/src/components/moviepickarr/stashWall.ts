import type { Movie } from "@/types/Response";

/**
 * The find-a-film path on an untitled wall.
 *
 * The wall drops the caption under the tile, so the field is the only way to
 * get from a title you can name to the poster that carries it — and the only
 * text the pane still has to compose. Both rules live here, away from the
 * markup, because both are about the term rather than about the layout.
 */

/** How much of the term the miss line echoes. It is raw user input. */
const TERM_CAP = 32;

/** The stash narrowed to what the term matches, by title, anywhere, any case. */
export function filterStash(stash: Movie[], filter: string): Movie[] {
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
