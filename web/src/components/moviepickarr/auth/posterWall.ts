// Pure layout logic behind the auth poster wall. The Marquee stays thin: it
// fetches the popularity-ordered poster paths and renders whatever posterWall()
// returns. Everything here is data-in/data-out (no DOM, no fetch), so it is
// unit-tested directly (posterWall.test.ts) the way authScreens is.

export const WALL_COLUMNS = 4;
export const WALL_ROWS = 4;

// Gradient poster stand-ins: four columns of four tiles, stored column-major so
// index = column * WALL_ROWS + row. They stand in for real artwork whenever the
// poster fetch is empty, still loading, or errored, so the wall is never broken.
// Each entry is two hues fed into the tile's diagonal gradient; the values match
// the original hand-tuned columns so the fallback looks unchanged.
export const TILES = [
  "197 58", "84 40", "150 55", "22 40",
  "84 62", "264 55", "310 45", "197 30",
  "264 30", "22 62", "84 55", "150 40",
  "22 40", "150 30", "264 45", "310 58",
];

// The centre of an n-wide axis in 0-based coordinates (1.5 for a 4-wide wall).
const centre = (n: number) => (n - 1) / 2;

// Popularity radiates from the visual middle: the slot nearest the wall's centre
// takes the #1 poster and the rest spiral outward by geometric distance. Ties
// break by flat index so the order is fully deterministic (unit-tested).
export const FAN_ORDER: number[] = TILES.map((_, i) => i)
  .map((i) => {
    const col = Math.floor(i / WALL_ROWS);
    const row = i % WALL_ROWS;
    return { i, d: Math.hypot(col - centre(WALL_COLUMNS), row - centre(WALL_ROWS)) };
  })
  .sort((a, b) => a.d - b.d || a.i - b.i)
  .map((e) => e.i);

export interface WallTile {
  /** Real TMDB poster path when a poster fills this slot, else null (gradient). */
  path: string | null;
  /** Duotone gradient hues — the stand-in, and the underlay behind every poster. */
  hues: string;
}

/**
 * Build the wall tiles (column-major) from the popularity-ordered poster paths.
 * The centre slot takes the most popular poster and the rest radiate outward
 * (FAN_ORDER); any slot left without a poster keeps its gradient stand-in, and
 * an empty array yields the pure gradient TILES fallback. Extra paths beyond the
 * wall's slot count are dropped.
 */
export function posterWall(paths: string[]): WallTile[] {
  const tiles: WallTile[] = TILES.map((hues) => ({ path: null, hues }));
  paths.slice(0, tiles.length).forEach((path, i) => {
    tiles[FAN_ORDER[i]].path = path;
  });
  return tiles;
}
