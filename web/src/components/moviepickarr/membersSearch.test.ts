import { describe, expect, it } from "vitest";

import {
  orderMembers,
  selectedMember,
  validateMembersSearch,
} from "@/components/moviepickarr/membersSearch";

import type { User } from "@/types/Response";

function member(userID: number, name = `Member ${userID}`): User {
  return { userID, name, currentPool: {}, stash: {}, createdAt: "2026-07-01T00:00:00Z" };
}

describe("validateMembersSearch", () => {
  it("takes a valid id", () => {
    expect(validateMembersSearch({ member: 4 })).toEqual({ member: 4 });
  });

  it("reads the id back off a URL, where it arrives as a string", () => {
    expect(validateMembersSearch({ member: "4" })).toEqual({ member: 4 });
  });

  it("drops a missing param, which the page reads as `me`", () => {
    expect(validateMembersSearch({})).toEqual({});
  });

  it("drops a non-numeric param rather than throwing", () => {
    expect(validateMembersSearch({ member: "ada" })).toEqual({});
  });

  it("drops a param that is neither a number nor a string", () => {
    expect(validateMembersSearch({ member: true })).toEqual({});
    expect(validateMembersSearch({ member: ["4"] })).toEqual({});
  });

  it("drops out-of-range ids: zero, negative, fractional and past the safe integer", () => {
    expect(validateMembersSearch({ member: 0 })).toEqual({});
    expect(validateMembersSearch({ member: -3 })).toEqual({});
    expect(validateMembersSearch({ member: 2.5 })).toEqual({});
    expect(validateMembersSearch({ member: "9007199254740993" })).toEqual({});
  });
});

describe("orderMembers", () => {
  const roster = [member(1), member(2), member(3)];

  it("puts the session member first and leaves the rest in the API's order", () => {
    expect(orderMembers(roster, 3).map((u) => u.userID)).toEqual([3, 1, 2]);
  });

  it("leaves the roster alone while the session is still loading", () => {
    expect(orderMembers(roster, undefined).map((u) => u.userID)).toEqual([1, 2, 3]);
  });

  it("survives a session member missing from the roster", () => {
    expect(orderMembers(roster, 9).map((u) => u.userID)).toEqual([1, 2, 3]);
  });

  it("has nothing to order before the roster lands", () => {
    expect(orderMembers(undefined, 1)).toEqual([]);
  });
});

describe("selectedMember", () => {
  const ordered = orderMembers([member(1), member(2), member(3)], 2);

  it("selects the member the URL names", () => {
    expect(selectedMember(ordered, 3, 2)?.userID).toBe(3);
  });

  it("selects your own board when the param is absent", () => {
    expect(selectedMember(ordered, undefined, 2)?.userID).toBe(2);
  });

  it("silently falls back to your own board on an id that does not resolve", () => {
    expect(selectedMember(ordered, 404, 2)?.userID).toBe(2);
  });

  it("falls back to the first row when the session has no board of its own", () => {
    const noSelf = orderMembers([member(1), member(2), member(3)], undefined);
    expect(selectedMember(noSelf, 404, undefined)?.userID).toBe(1);
  });

  it("has nothing to select on an empty roster", () => {
    expect(selectedMember([], undefined, 2)).toBeUndefined();
  });
});
