import { describe, expect, it } from "vitest";

import {
  FAN_ORDER,
  posterWall,
  TILES,
  WALL_COLUMNS,
  WALL_ROWS,
} from "@/components/moviepickarr/auth/posterWall";

describe("posterWall", () => {
  it("falls back to the pure gradient TILES when the array is empty", () => {
    const tiles = posterWall([]);
    expect(tiles).toHaveLength(TILES.length);
    expect(tiles.every((t) => t.path === null)).toBe(true);
    expect(tiles.map((t) => t.hues)).toEqual(TILES);
  });

  it("places the #1 poster at the centre and radiates outward", () => {
    const paths = ["/a.jpg", "/b.jpg", "/c.jpg"];
    const tiles = posterWall(paths);
    // Each path lands on its fan slot, most popular nearest the centre.
    expect(tiles[FAN_ORDER[0]].path).toBe("/a.jpg");
    expect(tiles[FAN_ORDER[1]].path).toBe("/b.jpg");
    expect(tiles[FAN_ORDER[2]].path).toBe("/c.jpg");
    // The remaining slots keep their gradient stand-in.
    expect(tiles.filter((t) => t.path !== null)).toHaveLength(3);
  });

  it("keeps the gradient hues as an underlay behind every poster", () => {
    const tiles = posterWall(["/a.jpg"]);
    expect(tiles[FAN_ORDER[0]]).toEqual({ path: "/a.jpg", hues: TILES[FAN_ORDER[0]] });
  });

  it("fills every slot when there are enough posters", () => {
    const paths = TILES.map((_, i) => `/p${i}.jpg`);
    const tiles = posterWall(paths);
    expect(tiles.every((t) => t.path !== null)).toBe(true);
  });

  it("drops posters beyond the wall's slot count", () => {
    const paths = Array.from({ length: TILES.length + 5 }, (_, i) => `/p${i}.jpg`);
    const tiles = posterWall(paths);
    expect(tiles).toHaveLength(TILES.length);
    expect(tiles.every((t) => t.path !== null)).toBe(true);
    // Only the first slot-count posters make it onto the wall.
    expect(tiles.map((t) => t.path)).not.toContain(`/p${TILES.length}.jpg`);
  });

  it("has a centre-first fan order (inner 2x2 nearest the middle)", () => {
    // The four innermost slots sit closest to the centre, so they take the top
    // four popularity ranks before any edge or corner slot.
    const innerSlots = new Set<number>();
    for (let col = 1; col <= 2; col++) {
      for (let row = 1; row <= 2; row++) innerSlots.add(col * WALL_ROWS + row);
    }
    expect(new Set(FAN_ORDER.slice(0, 4))).toEqual(innerSlots);
    expect(FAN_ORDER).toHaveLength(WALL_COLUMNS * WALL_ROWS);
  });
});
