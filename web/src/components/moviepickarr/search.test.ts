import { describe, expect, it } from "vitest";

import { chunkRows, filterChoices, filterWatched, normalizeQuery } from "@/components/moviepickarr/search";

const movie = (title: string, addedByName: string) => ({ title, addedByName });

describe("normalizeQuery", () => {
  it("trims and case-folds", () => {
    expect(normalizeQuery("  Dune  ")).toBe("dune");
  });

  it("treats whitespace-only input as no query", () => {
    expect(normalizeQuery("   ")).toBe("");
  });
});

describe("filterWatched", () => {
  const watched = [movie("Dune", "Ada"), movie("Heat", "Bo"), movie("The Duellists", "Cy")];

  it("returns the input untouched when the query is blank", () => {
    expect(filterWatched(watched, "  ")).toBe(watched);
  });

  it("matches titles case-insensitively on a substring", () => {
    expect(filterWatched(watched, "DUN").map((m) => m.title)).toEqual(["Dune"]);
    expect(filterWatched(watched, "duel").map((m) => m.title)).toEqual(["The Duellists"]);
  });

  it("matches the adder's name", () => {
    expect(filterWatched(watched, "bo").map((m) => m.title)).toEqual(["Heat"]);
  });

  it("returns nothing when nothing matches", () => {
    expect(filterWatched(watched, "zzz")).toEqual([]);
  });
});

describe("filterChoices", () => {
  const choices = [{ label: "Denis Villeneuve" }, { label: "Michael Mann" }, { label: "Ridley Scott" }];

  it("returns the input untouched when the query is blank", () => {
    expect(filterChoices(choices, "")).toBe(choices);
  });

  it("matches labels case-insensitively on a substring", () => {
    expect(filterChoices(choices, "mann")).toEqual([{ label: "Michael Mann" }]);
  });
});

describe("chunkRows", () => {
  const items = Array.from({ length: 7 }, (_, i) => i);

  it("splits a list into rows of the lane count, last row short", () => {
    expect(chunkRows(items, 3)).toEqual([
      [0, 1, 2],
      [3, 4, 5],
      [6],
    ]);
  });

  it("gives one row per item at a single lane", () => {
    expect(chunkRows(items, 1)).toHaveLength(7);
  });

  it("has no rows for an empty list", () => {
    expect(chunkRows([], 4)).toEqual([]);
  });

  it("falls back to one lane on a zero or negative count", () => {
    // A pre-measurement render can hand us 0 lanes; it must not loop forever.
    expect(chunkRows(items, 0)).toHaveLength(7);
    expect(chunkRows(items, -3)).toHaveLength(7);
  });

  it("keeps every item exactly once, in order", () => {
    expect(chunkRows(items, 4).flat()).toEqual(items);
  });
});
