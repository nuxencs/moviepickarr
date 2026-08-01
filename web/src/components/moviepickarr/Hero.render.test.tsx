/* ============================================================
   Render tests for the hero banner's attribution (#238).

   The reveal machinery, the turn gate and the draw reel all have their own
   homes (drawMachine.test.ts, turnGate.test.ts, DrawReel.render.test.tsx), and
   none of it is repeated here. What is here is the one thing only the rendered
   banner can answer: that the adder's name in the eyebrow is the way to their
   board, and that following it stacks a history entry instead of spending one.

   The draw is seeded without a backdrop on purpose: with one, the commit waits
   on an image decode that jsdom will not run, and the banner never reaches its
   revealed state.
   ============================================================ */

import { fireEvent, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { APIClient } from "@/api/APIClient";
import { AuthKeys, MoviesKeys, SettingsKeys } from "@/api/query_keys";

import { Hero } from "@/components/moviepickarr/Hero";

import type { MeResponse, Movie } from "@/types/Response";
import type { QueryClient } from "@tanstack/react-query";

import { renderWithProviders } from "@/test/providers";

vi.mock("@/api/APIClient", () => ({
  APIClient: {
    movies: { getCurrent: vi.fn(), getPool: vi.fn(), draw: vi.fn(), markWatched: vi.fn() },
    settings: { getNextUp: vi.fn() },
    auth: { me: vi.fn() },
  },
  ApiError: class ApiError extends Error {},
}));

const drawn: Movie = {
  movieID: 42,
  title: "Apocalypse Now",
  link: "",
  addedAt: "2026-07-22T10:00:00Z",
  addedByID: 7,
  addedByName: "Ada",
  drawnAt: "2026-07-28T20:00:00Z",
};

function session(id: number): MeResponse {
  return {
    id,
    displayName: `Member ${id}`,
    username: null,
    role: "admin",
    hasLocalLogin: true,
    hasLinkedIdentity: false,
    otherSessions: 0,
  };
}

/** The banner on the Movies page with a draw already up. */
async function renderHero() {
  let queryClient: QueryClient | undefined;
  const view = await renderWithProviders(<Hero />, {
    path: "/",
    seed: (client) => {
      queryClient = client;
      client.setQueryData(MoviesKeys.current(), drawn);
      client.setQueryData(MoviesKeys.listpool(), []);
      client.setQueryData(SettingsKeys.nextUp(), { id: 1, name: "Member 1" });
      client.setQueryData(SettingsKeys.poolLock(), {
        poolLocked: false,
        drawInProgress: true,
      });
      client.setQueryData(AuthKeys.me(), session(1));
    },
  });
  return { ...view, queryClient: queryClient! };
}

/** The adder's name, once the draw has been revealed into the banner. */
async function adder() {
  return waitFor(() => screen.getByRole("link", { name: "Ada" }));
}

describe("the hero's attribution", () => {
  it("points the adder's name at their board", async () => {
    await renderHero();

    expect((await adder()).getAttribute("href")).toBe("/users?member=7");
  });

  it("stacks the navigation, so Back comes back to the draw", async () => {
    const { router } = await renderHero();

    fireEvent.click(await adder());

    await waitFor(() => expect(router.state.location.href).toBe("/users?member=7"));
    // A push, not the modal's replace: nothing here is holding a history entry
    // the link has to spend, so leaving the banner is an ordinary navigation.
    router.history.back();
    await waitFor(() => expect(router.state.location.href).toBe("/"));
  });

  it("releases the cached draw gate when its own watched event is lost", async () => {
    vi.mocked(APIClient.movies.markWatched).mockResolvedValueOnce({
      ...drawn,
      status: "watched",
    });
    const { queryClient } = await renderHero();

    await adder();
    fireEvent.click(screen.getByRole("button", { name: "Mark as watched" }));

    await waitFor(() =>
      expect(
        queryClient.getQueryData<{ drawInProgress: boolean }>(SettingsKeys.poolLock())
          ?.drawInProgress,
      ).toBe(false),
    );
  });
});
