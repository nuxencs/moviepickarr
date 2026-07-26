import { describe, expect, it } from "vitest";

import { filterStash, missLine } from "@/components/moviepickarr/stashWall";

import type { Movie } from "@/types/Response";

function film(movieID: number, title: string): Movie {
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
