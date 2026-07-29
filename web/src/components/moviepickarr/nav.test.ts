import { describe, expect, it } from "vitest";

import { tabFromPath, tabsForRole } from "@/components/moviepickarr/nav";

describe("tabsForRole", () => {
  it("gives an admin all four tabs in order", () => {
    expect(tabsForRole("admin").map((t) => t.id)).toEqual(["movies", "users", "stats", "admin"]);
  });

  it("hides Admin from a member", () => {
    expect(tabsForRole("member").map((t) => t.id)).toEqual(["movies", "users", "stats"]);
  });

  it("hides Admin when the role is unknown (logged out)", () => {
    expect(tabsForRole(undefined).map((t) => t.id)).toEqual(["movies", "users", "stats"]);
  });
});

describe("tabFromPath", () => {
  it("maps the admin path to Admin", () => {
    expect(tabFromPath("/admin")).toBe("admin");
    expect(tabFromPath("/admin/whatever")).toBe("admin");
  });

  it("maps the stats and users paths", () => {
    expect(tabFromPath("/stats")).toBe("stats");
    expect(tabFromPath("/users")).toBe("users");
  });

  it("maps root to Movies", () => {
    expect(tabFromPath("/")).toBe("movies");
  });

  it("highlights no tab on non-tab pages like account settings", () => {
    expect(tabFromPath("/settings")).toBeNull();
    expect(tabFromPath("/anything-else")).toBeNull();
  });
});
