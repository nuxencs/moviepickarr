/**
 * Query matching for the two search-driven lists — the MoviesTab watched grid
 * and the FilterBar option menus.
 *
 * Both lists grow with the library (watched films, people credited on them), so
 * the components defer the typed query into these helpers (useDeferredValue)
 * and render the result through a virtualizer: filtering stays off the
 * keystroke's critical path, and only the rows in view ever hit the DOM.
 */

/** Trimmed, case-folded query; empty means "no filter". */
export function normalizeQuery(query: string): string {
  return query.trim().toLowerCase();
}

/** A watched film matches on its title or the name of whoever added it. */
export function filterWatched<T extends { title: string; addedByName: string }>(
  movies: readonly T[],
  query: string,
): readonly T[] {
  const q = normalizeQuery(query);
  if (!q) return movies;
  return movies.filter((m) => m.title.toLowerCase().includes(q) || m.addedByName.toLowerCase().includes(q));
}

/** A filter-menu option matches on its visible label. */
export function filterChoices<T extends { label: string }>(
  choices: readonly T[],
  query: string,
): readonly T[] {
  const q = normalizeQuery(query);
  if (!q) return choices;
  return choices.filter((c) => c.label.toLowerCase().includes(q));
}

/** Split a flat list into rows of `size` — one virtualized row per grid row. */
export function chunkRows<T>(items: readonly T[], size: number): T[][] {
  const width = Math.max(1, Math.floor(size));
  const rows: T[][] = [];
  for (let i = 0; i < items.length; i += width) rows.push(items.slice(i, i + width));
  return rows;
}
