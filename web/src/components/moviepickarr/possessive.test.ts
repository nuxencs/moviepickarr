import { describe, expect, it } from "vitest";

import { possessive } from "@/components/moviepickarr/possessive";

describe("possessive", () => {
  it("appends 's to a name that doesn't end in s", () => {
    expect(possessive("Ada")).toBe("Ada's");
    expect(possessive("Bob")).toBe("Bob's");
  });

  it("appends only an apostrophe to a name ending in s", () => {
    expect(possessive("Aleks")).toBe("Aleks'");
    expect(possessive("Chris")).toBe("Chris'");
    expect(possessive("James")).toBe("James'");
  });

  it("treats a trailing S case-insensitively", () => {
    expect(possessive("ROSS")).toBe("ROSS'");
    expect(possessive("lucas")).toBe("lucas'");
  });

  it("returns an empty string for an empty name", () => {
    expect(possessive("")).toBe("");
  });
});
