import type { MovieTile } from "@/types/Response";

/**
 * Search, order, and keyboard rules for the untitled stash wall, kept out of the markup
 * so they test without rendering (#235).
 */

/** How much of the term the miss line echoes. It is raw user input. */
const TERM_CAP = 32;

/** The stash narrowed to titles containing the term, any case. */
export function filterStash(stash: MovieTile[], filter: string): MovieTile[] {
  const q = filter.trim().toLowerCase();
  // Same array back: the wall is memoized per tile, so a new identity would re-render it.
  if (!q) return stash;
  return stash.filter((movie) => movie.title.toLowerCase().includes(q));
}

/** The viewer's stash order. Only keys every movie has from the start (#254). */
export type StashOrder = "title-asc" | "title-desc" | "added-desc" | "added-asc";

/** Wording of every stash order, in menu order. */
export const STASH_ORDER_LABELS: Record<StashOrder, string> = {
  "title-asc": "Title A–Z",
  "title-desc": "Title Z–A",
  "added-desc": "Newest added",
  "added-asc": "Oldest added",
};

export const STASH_ORDER_KEYS = Object.keys(STASH_ORDER_LABELS) as StashOrder[];

const ORDER_KEY = "mp-stash-order";

/** The order this browser last chose, or title A to Z. */
export function readStashOrder(): StashOrder {
  try {
    const stored = localStorage.getItem(ORDER_KEY);
    return STASH_ORDER_KEYS.find((order) => order === stored) ?? "title-asc";
  } catch {
    // Blocked storage (private mode, site data off) reads as the default.
    return "title-asc";
  }
}

/** Keeps the order for this browser. */
export function writeStashOrder(order: StashOrder): void {
  try {
    localStorage.setItem(ORDER_KEY, order);
  } catch {
    // Blocked storage keeps the order for this visit only.
  }
}

const byTitle = (a: MovieTile, b: MovieTile) =>
  a.title.localeCompare(b.title) || a.movieID - b.movieID;
const byAdded = (a: MovieTile, b: MovieTile) => Date.parse(a.addedAt) - Date.parse(b.addedAt);

/**
 * A sorted copy of the stash. Every tie resolves, so all viewers of one order
 * see one wall; a date tie reads A to Z in both directions.
 */
export function sortStash(stash: MovieTile[], order: StashOrder): MovieTile[] {
  const compare = {
    "title-asc": byTitle,
    "title-desc": (a: MovieTile, b: MovieTile) => byTitle(b, a),
    "added-desc": (a: MovieTile, b: MovieTile) => byAdded(b, a) || byTitle(a, b),
    "added-asc": (a: MovieTile, b: MovieTile) => byAdded(a, b) || byTitle(a, b),
  }[order];
  return [...stash].sort(compare);
}

/** What a wall with no hits says, the same on every member's board. */
export function missLine(filter: string): string {
  const term = filter.trim();
  const shown = term.length > TERM_CAP ? `${term.slice(0, TERM_CAP)}…` : term;
  return `Nothing matches "${shown}"`;
}

/**
 * The cell a key reaches from `from`, or null for an unhandled key or a move
 * off the end. Left/Right wrap across rows (the wall is one A-Z run, not a
 * `role="grid"`). Moves are refused, not clamped: a Down clamped onto a short
 * last row would also move sideways.
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
 * Where focus goes after the movie at `vacated` leaves a band of `left` cells:
 * the cell that slides in, or the one before when it was the end. Null means
 * nothing is left; each band picks its own fallback.
 */
export function landingCell(vacated: number, left: number): number | null {
  if (left <= 0) return null;
  return Math.min(Math.max(vacated, 0), left - 1);
}

/**
 * How many columns a computed `grid-template-columns` describes. Read from CSS
 * because the count comes from a container query; floors at one.
 */
export function columnCount(template: string): number {
  const tracks = template.trim();
  if (!tracks || tracks === "none") return 1;
  return tracks.split(/\s+/).length;
}
