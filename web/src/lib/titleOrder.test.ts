import { describe, expect, it } from "vitest";

import { compareTitles } from "@/lib/titleOrder";

const sorted = (titles: string[]) => [...titles].sort(compareTitles);

describe("compareTitles", () => {
  it("ignores a leading The, A or An", () => {
    expect(
      sorted(["The Matrix", "Memento", "A Quiet Place", "Alien", "Blade Runner", "The Batman"]),
    ).toEqual(["Alien", "The Batman", "Blade Runner", "The Matrix", "Memento", "A Quiet Place"]);
  });

  it("ignores the article in any case", () => {
    expect(sorted(["the Zoo", "AN Apple", "a Moon"])).toEqual(["AN Apple", "a Moon", "the Zoo"]);
  });

  it("keeps a first word that only starts with article letters", () => {
    expect(sorted(["Theory of Everything", "Anora", "Matrix", "Alien"])).toEqual([
      "Alien",
      "Anora",
      "Matrix",
      "Theory of Everything",
    ]);
  });

  it("keeps a title that is only the article", () => {
    expect(sorted(["Bambi", "The", "A"])).toEqual(["A", "Bambi", "The"]);
  });

  it("breaks a tie after the article by the full title", () => {
    expect(compareTitles("The Thing", "Thing")).toBeLessThan(0);
    expect(compareTitles("Thing", "The Thing")).toBeGreaterThan(0);
    expect(compareTitles("The Thing", "The Thing")).toBe(0);
  });

  it("orders numbers by value", () => {
    expect(sorted(["Saw 10", "Saw 2"])).toEqual(["Saw 2", "Saw 10"]);
  });
});
