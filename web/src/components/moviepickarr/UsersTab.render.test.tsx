/* ============================================================
   Render test for the Members page status line (#230).

   Which words the line uses is a pure question and poolLock.test.ts owns it:
   the whole state table is asserted there against membersStatus, and none of
   it is repeated here. What that test can't see is the split that makes the
   line bearable to listen to: the visible span carries every clause and is
   silent, while a visually-hidden role="status" carries the round and draw
   clauses alone. Merge them and every promote arriving over SSE re-reads the
   whole string at anyone using a screen reader.

   So this file checks the split survives the trip into the DOM: the live
   region is the hidden one, occupancy moving announces nothing, and the head
   states no member count until it has a roster to count.
   ============================================================ */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { SettingsKeys, UsersKeys } from "@/api/query_keys";

import { UsersTab } from "@/components/moviepickarr/UsersTab";

import type { Movie, User } from "@/types/Response";

vi.mock("@/api/APIClient", () => ({
  APIClient: {
    board: { getAll: vi.fn(), moveMovie: vi.fn(), deleteMovie: vi.fn(), updateMovie: vi.fn() },
    settings: { getLock: vi.fn() },
    auth: { me: vi.fn() },
  },
}));

function movie(movieID: number): Movie {
  return {
    movieID,
    title: `Film ${movieID}`,
    link: "",
    addedAt: "2026-07-01T00:00:00Z",
    addedByID: 1,
    addedByName: "Cleo",
  };
}

/** A member with `pooled` of their three slots filled. */
function member(userID: number, pooled: number): User {
  const currentPool: Record<string, Movie> = {};
  for (let i = 0; i < pooled; i++) currentPool[`${userID}${i}`] = movie(userID * 10 + i);
  return { userID, name: `Member ${userID}`, currentPool, stash: {}, createdAt: "2026-07-01T00:00:00Z" };
}

function renderTab({ users, locked = false }: { users?: User[]; locked?: boolean }) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Infinity } },
  });
  // Seeded rather than fetched: the line is the subject, not the requests.
  if (users) client.setQueryData(UsersKeys.list(), users);
  client.setQueryData(SettingsKeys.poolLock(), locked);

  render(
    <QueryClientProvider client={client}>
      <UsersTab />
    </QueryClientProvider>,
  );
  return client;
}

/** The visually-hidden live region, whatever it currently says. */
const liveRegion = () => document.querySelector('[role="status"]');

describe("the Members status line", () => {
  it("puts every clause on the visible span and keeps it out of the live region", () => {
    renderTab({ users: [member(1, 3), member(2, 3)], locked: true });

    const visible = document.querySelector(".sec-status");
    expect(visible?.textContent).toBe("6 of 6 slots filled · round closed");
    expect(visible?.getAttribute("role")).toBeNull();
    expect(visible?.getAttribute("aria-live")).toBeNull();

    // The round clause alone reaches the region, and the region is hidden.
    expect(liveRegion()?.textContent).toBe("round closed");
    expect(liveRegion()?.className).toContain("vis-hidden");
  });

  it("announces nothing when occupancy moves", async () => {
    const client = renderTab({ users: [member(1, 1), member(2, 0)] });
    expect(document.querySelector(".sec-status")?.textContent).toBe("1 of 6 slots filled");
    expect(liveRegion()?.textContent).toBe("");

    // Somebody else's promote arriving over SSE.
    client.setQueryData(UsersKeys.list(), [member(1, 1), member(2, 1)]);

    await waitFor(() =>
      expect(document.querySelector(".sec-status")?.textContent).toBe("2 of 6 slots filled"),
    );
    expect(liveRegion()?.textContent).toBe("");
  });

  it("says ready to lock, out loud, once every pool fills", async () => {
    const client = renderTab({ users: [member(1, 3), member(2, 2)] });
    expect(liveRegion()?.textContent).toBe("");

    client.setQueryData(UsersKeys.list(), [member(1, 3), member(2, 3)]);

    await waitFor(() => expect(liveRegion()?.textContent).toBe("ready to lock"));
  });

  it("heads a pending roster with a bare Members and no announcement", () => {
    renderTab({});

    expect(screen.getByRole("heading", { name: "Members" })).toBeTruthy();
    expect(document.querySelector(".sec-count")).toBeNull();
    expect(document.querySelector(".sec-status")).toBeNull();
    expect(liveRegion()?.textContent).toBe("");
  });
});
