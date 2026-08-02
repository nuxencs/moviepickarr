import { describe, expect, it } from "vitest";

import {
  columnCount,
  filterStash,
  landingCell,
  missLine,
  nextCell,
} from "@/components/moviepickarr/stashWall";

import type { MovieTile } from "@/types/Response";

function film(movieID: number, title: string): MovieTile {
  return {
    movieID,
    title,
    link: "",
    addedAt: "2026-07-01T00:00:00Z",
    addedByID: 1,
    addedByName: "Ada",
  };
}

const stash = [film(1, "Dune"), film(2, "Dune: Part Two"), film(3, "Arrival")];

describe("filterStash", () => {
  it("keeps the whole stash when nothing is typed", () => {
    expect(filterStash(stash, "")).toBe(stash);
    expect(filterStash(stash, "   ")).toBe(stash);
  });

  it("matches anywhere in the title, ignoring case and surrounding space", () => {
    expect(filterStash(stash, " dUNe ").map((m) => m.movieID)).toEqual([1, 2]);
    expect(filterStash(stash, "part").map((m) => m.movieID)).toEqual([2]);
  });

  it("returns nothing when no title matches", () => {
    expect(filterStash(stash, "zzz")).toEqual([]);
  });
});

describe("missLine", () => {
  it("echoes the term back in quotes", () => {
    expect(missLine("dune")).toBe('Nothing matches "dune"');
  });

  it("echoes what was typed, not what was searched", () => {
    // Trimmed, because the quotes would otherwise show the space as a gap the
    // user cannot account for.
    expect(missLine("  dune  ")).toBe('Nothing matches "dune"');
  });

  it("caps the echo, since the term is raw user input", () => {
    const long = "a".repeat(200);
    expect(missLine(long)).toBe(`Nothing matches "${"a".repeat(32)}…"`);
  });
});

/* Moving around the wall with the keyboard (#235). Fourteen cells over six
   columns: two full rows and a two-cell third one, so every edge the wall has
   is in reach of one case. */
describe("nextCell", () => {
  const CELLS = 14;
  const COLS = 6;
  const from = (index: number, key: string) => nextCell(key, index, CELLS, COLS);

  it("steps a film at a time left and right, in the wall's own order", () => {
    expect(from(0, "ArrowRight")).toBe(1);
    expect(from(6, "ArrowLeft")).toBe(5);
    // Across a row boundary, because the order the wall reads in is one run and
    // the supplied column count is the stylesheet's business.
    expect(from(5, "ArrowRight")).toBe(6);
  });

  it("steps a row at a time up and down", () => {
    expect(from(1, "ArrowDown")).toBe(7);
    expect(from(7, "ArrowUp")).toBe(1);
  });

  it("goes to the ends on Home and End, from anywhere", () => {
    expect(from(7, "Home")).toBe(0);
    expect(from(7, "End")).toBe(13);
    expect(from(0, "Home")).toBe(0);
    expect(from(13, "End")).toBe(13);
  });

  it("refuses a move off the end rather than clamping it", () => {
    expect(from(0, "ArrowLeft")).toBeNull();
    expect(from(13, "ArrowRight")).toBeNull();
    expect(from(2, "ArrowUp")).toBeNull();
    // Down out of the last full row, over a third row that is two cells long:
    // no cell there, so focus stays put rather than sliding to the last film.
    expect(from(9, "ArrowDown")).toBeNull();
    expect(from(6, "ArrowDown")).toBe(12);
  });

  it("answers nothing for a key that is not the wall's", () => {
    expect(from(3, "Enter")).toBeNull();
    expect(from(3, "PageDown")).toBeNull();
    expect(from(3, "a")).toBeNull();
  });

  it("has nowhere to go on an empty wall", () => {
    expect(nextCell("Home", 0, 0, 6)).toBeNull();
    expect(nextCell("ArrowRight", 0, 0, 6)).toBeNull();
  });

  it("is a single column when the wall is one film wide", () => {
    expect(nextCell("ArrowDown", 0, 3, 1)).toBe(1);
    expect(nextCell("ArrowRight", 0, 3, 1)).toBe(1);
  });
});

describe("landingCell", () => {
  it("hands focus to the cell that slid into the vacated index", () => {
    expect(landingCell(2, 5)).toBe(2);
  });

  it("falls back to the cell before it when the end was vacated", () => {
    expect(landingCell(5, 5)).toBe(4);
    expect(landingCell(9, 3)).toBe(2);
  });

  it("says there is nowhere to land when the band is empty", () => {
    expect(landingCell(0, 0)).toBeNull();
  });
});

describe("columnCount", () => {
  it("counts the tracks the browser resolved", () => {
    expect(columnCount("96px 96px 96px 96px 96px 96px")).toBe(6);
    expect(columnCount("  128px 128px  ")).toBe(2);
  });

  it("floors at one column when there is nothing to read", () => {
    // jsdom has no layout and no stylesheet, and a wall that has not been laid
    // out yet answers `none`. One column still moves, a film at a time.
    expect(columnCount("")).toBe(1);
    expect(columnCount("none")).toBe(1);
  });
});
