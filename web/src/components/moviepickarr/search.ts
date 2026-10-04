// Query matching for the watched grid and the FilterBar menus. Callers defer the
// query (useDeferredValue) and virtualize, as both lists grow with the library.

/** Empty means "no filter". */
export function normalizeQuery(query: string): string {
  return query.trim().toLowerCase();
}

/** Matches on title or adder name. */
export function filterWatched<T extends { title: string; addedByName: string }>(
  movies: readonly T[],
  query: string,
): readonly T[] {
  const q = normalizeQuery(query);
  if (!q) return movies;
  return movies.filter((m) => m.title.toLowerCase().includes(q) || m.addedByName.toLowerCase().includes(q));
}

export function filterChoices<T extends { label: string }>(
  choices: readonly T[],
  query: string,
): readonly T[] {
  const q = normalizeQuery(query);
  if (!q) return choices;
  return choices.filter((c) => c.label.toLowerCase().includes(q));
}

/** One virtualized row per grid row. */
export function chunkRows<T>(items: readonly T[], size: number): T[][] {
  const width = Math.max(1, Math.floor(size));
  const rows: T[][] = [];
  for (let i = 0; i < items.length; i += width) rows.push(items.slice(i, i + width));
  return rows;
}
