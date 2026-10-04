export const WALL_COLUMNS = 4;
export const WALL_ROWS = 4;

// Gradient stand-in hue pairs, column-major (index = column * WALL_ROWS + row).
export const TILES = [
  "197 58", "84 40", "150 55", "22 40",
  "84 62", "264 55", "310 45", "197 30",
  "264 30", "22 62", "84 55", "150 40",
  "22 40", "150 30", "264 45", "310 58",
];

const centre = (n: number) => (n - 1) / 2;

// The #1 poster takes the centre slot and the rest radiate outward; ties break by
// index so the order is deterministic.
export const FAN_ORDER: number[] = TILES.map((_, i) => i)
  .map((i) => {
    const col = Math.floor(i / WALL_ROWS);
    const row = i % WALL_ROWS;
    return { i, d: Math.hypot(col - centre(WALL_COLUMNS), row - centre(WALL_ROWS)) };
  })
  .sort((a, b) => a.d - b.d || a.i - b.i)
  .map((e) => e.i);

export interface WallTile {
  path: string | null;
  /** Gradient stand-in hues, also the underlay behind every poster. */
  hues: string;
}

/** Fills slots in FAN_ORDER from popularity-ordered paths; empty slots keep their gradient. */
export function posterWall(paths: string[]): WallTile[] {
  const tiles: WallTile[] = TILES.map((hues) => ({ path: null, hues }));
  paths.slice(0, tiles.length).forEach((path, i) => {
    tiles[FAN_ORDER[i]].path = path;
  });
  return tiles;
}
