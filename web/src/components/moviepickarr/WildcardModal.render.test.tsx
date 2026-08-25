import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { APIClient } from "@/api/APIClient";
import { UsersKeys } from "@/api/query_keys";

import { WildcardModal } from "@/components/moviepickarr/WildcardModal";

import type { User } from "@/types/Response";

import { renderWithProviders } from "@/test/providers";

vi.mock("@/api/APIClient", () => ({
  APIClient: {
    board: { getAll: vi.fn() },
    movies: { selectWildcard: vi.fn(), selectTMDBWildcard: vi.fn() },
    tmdb: { search: vi.fn() },
  },
}));

const members: User[] = [{
  userID: 1,
  name: "Ada",
  currentPool: {
    "12": {
      movieID: 12,
      title: "Heat",
      link: "",
      addedAt: "2026-08-25T10:00:00Z",
      addedByID: 1,
      addedByName: "Ada",
    },
  },
  stash: {},
  createdAt: "2026-08-01T10:00:00Z",
}];

describe("the wildcard picker", () => {
  it("lets any library movie become the wildcard", async () => {
    vi.mocked(APIClient.board.getAll).mockResolvedValue(members);
    vi.mocked(APIClient.movies.selectWildcard).mockResolvedValue({
      id: 3,
      hostMovieId: 7,
      selectedAt: "2026-08-25T18:00:00Z",
      movie: { ...members[0].currentPool["12"], status: "wildcard" },
    });

    await renderWithProviders(<WildcardModal hostMovieID={7} onClose={vi.fn()} />, {
      path: "/",
      seed: (client) => client.setQueryData(UsersKeys.list(), members),
    });

    const result = screen.getByText("Heat", { selector: ".r-title" }).closest(".result");
    expect(result).not.toBeNull();
    fireEvent.click(within(result as HTMLElement).getByRole("button", { name: "Choose" }));

    await waitFor(() => expect(APIClient.movies.selectWildcard).toHaveBeenCalledWith(7, 12));
  });

  it("can select a TMDB result that is not in a stash", async () => {
    vi.mocked(APIClient.board.getAll).mockResolvedValue([]);
    vi.mocked(APIClient.tmdb.search).mockResolvedValue([{ id: 99, title: "Guest Night", poster_path: null, release_date: "2026-01-01", overview: "" }]);
    vi.mocked(APIClient.movies.selectTMDBWildcard).mockResolvedValue({
      id: 4,
      hostMovieId: 7,
      selectedAt: "2026-08-25T18:00:00Z",
      movie: {
        movieID: 30, title: "Guest Night", link: "", addedAt: "2026-08-25T18:00:00Z",
        addedByID: 1, addedByName: "Ada", status: "wildcard",
      },
    });

    await renderWithProviders(<WildcardModal hostMovieID={7} onClose={vi.fn()} />, {
      path: "/",
      seed: (client) => client.setQueryData(UsersKeys.list(), []),
    });
    fireEvent.change(screen.getByRole("textbox", { name: "Search the library and TMDB" }), { target: { value: "Guest Night" } });
    fireEvent.click(screen.getByRole("button", { name: "Search TMDB" }));

    const result = (await screen.findByText("Guest Night", { selector: ".r-title" })).closest(".result");
    fireEvent.click(within(result as HTMLElement).getByRole("button", { name: "Choose" }));
    await waitFor(() => expect(APIClient.movies.selectTMDBWildcard).toHaveBeenCalledWith(7, "Guest Night", 99));
  });
});
