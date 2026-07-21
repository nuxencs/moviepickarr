import { describe, expect, it } from "vitest";

import { tabFromPath, tabsForRole } from "@/components/moviepickarr/nav";

describe("tabsForRole", () => {
  it("gives an admin all four tabs in order", () => {
    expect(tabsForRole("admin").map((t) => t.id)).toEqual(["movies", "users", "stats", "roster"]);
  });

  it("hides Roster from a member", () => {
    expect(tabsForRole("member").map((t) => t.id)).toEqual(["movies", "users", "stats"]);
  });

  it("hides Roster when the role is unknown (logged out)", () => {
    expect(tabsForRole(undefined).map((t) => t.id)).toEqual(["movies", "users", "stats"]);
  });
});

describe("tabFromPath", () => {
  it("maps the admin roster path to Roster", () => {
    expect(tabFromPath("/admin")).toBe("roster");
    expect(tabFromPath("/admin/whatever")).toBe("roster");
  });

  it("maps the stats and users paths", () => {
    expect(tabFromPath("/stats")).toBe("stats");
    expect(tabFromPath("/users")).toBe("users");
  });

  it("falls through to Movies for root and unknown paths", () => {
    expect(tabFromPath("/")).toBe("movies");
    expect(tabFromPath("/settings")).toBe("movies");
    expect(tabFromPath("/anything-else")).toBe("movies");
  });
});
