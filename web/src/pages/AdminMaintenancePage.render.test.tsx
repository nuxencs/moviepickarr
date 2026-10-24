import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { APIClient, ApiError } from "@/api/APIClient";

import { toast } from "@/components/ui/toast-api";

import type { LibraryMovie, TMDBMovie } from "@/types/Response";

import { AdminMaintenancePage } from "@/pages/AdminMaintenancePage";

vi.mock("@/api/APIClient", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api/APIClient")>()),
  APIClient: {
    maintenance: { library: vi.fn(), correctMovie: vi.fn() },
    tmdb: { search: vi.fn() },
  },
}));

// jsdom has no layout, so every row renders; Playwright checks the real window.
vi.mock("@tanstack/react-virtual", () => ({
  useVirtualizer: (options: { count: number }) => ({
    getTotalSize: () => options.count * 81,
    getVirtualItems: () =>
      Array.from({ length: options.count }, (_, index) => ({ key: index, index, start: index * 81 })),
    measureElement: () => {},
  }),
}));

vi.mock("@/components/ui/toast-api", () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}));

const library: LibraryMovie[] = [
  {
    movieID: 1,
    title: "Wrong Pick",
    link: "",
    addedAt: "2026-01-01T00:00:00Z",
    addedByID: 7,
    addedByName: "Ada",
    tmdbId: 100,
    releaseDate: "1998-05-01",
    status: "stash",
  },
  {
    movieID: 2,
    title: "Alien",
    link: "",
    addedAt: "2026-01-02T00:00:00Z",
    addedByID: 8,
    addedByName: "Grace",
    tmdbId: 348,
    releaseDate: "1979-05-25",
    watchedAt: "2026-02-07T13:00:37.123Z",
    status: "watched",
  },
];

const rightPick: TMDBMovie = {
  id: 200,
  title: "Right Pick",
  overview: "",
  poster_path: null,
  release_date: "1999-03-30",
};

function renderPage() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <AdminMaintenancePage />
    </QueryClientProvider>,
  );
}

async function openCorrection(title: string) {
  fireEvent.click(await screen.findByRole("button", { name: new RegExp(`^Correct ${title}`) }));
  return screen.getByRole("dialog", { name: "Correct movie" });
}

function withTZ(tz: string, run: () => void | Promise<void>) {
  const original = process.env.TZ;
  process.env.TZ = tz;
  const restore = () => {
    if (original === undefined) delete process.env.TZ;
    else process.env.TZ = original;
  };
  return Promise.resolve(run()).finally(restore);
}

beforeEach(() => {
  vi.mocked(APIClient.maintenance.library).mockResolvedValue(library);
  vi.mocked(APIClient.maintenance.correctMovie).mockResolvedValue({} as never);
  vi.mocked(APIClient.tmdb.search).mockResolvedValue([rightPick]);
});

afterEach(() => {
  vi.clearAllMocks();
});

describe("AdminMaintenancePage library", () => {
  it("lists every movie A to Z with its status, adder, and TMDB id", async () => {
    renderPage();

    const list = await screen.findByRole("list", { name: "Movie library" });
    const rows = within(list).getAllByRole("button");
    expect(rows.map((row) => row.getAttribute("aria-label"))).toEqual([
      "Correct Alien",
      "Correct Wrong Pick",
    ]);
    expect(within(rows[0]).getByText("Watched")).not.toBeNull();
    expect(within(rows[0]).getByText("Grace")).not.toBeNull();
    expect(within(rows[0]).getByText("348")).not.toBeNull();
    expect(screen.getByText("2 movies")).not.toBeNull();
  });

  it("filters by title or adder", async () => {
    renderPage();
    await screen.findByRole("list", { name: "Movie library" });

    const search = screen.getByRole("searchbox", { name: "Search the movie library" });
    fireEvent.change(search, { target: { value: "ada" } });

    await waitFor(() =>
      expect(screen.queryByRole("button", { name: "Correct Alien" })).toBeNull(),
    );
    expect(screen.getByRole("button", { name: "Correct Wrong Pick" })).not.toBeNull();
  });

  it("explains a refused library read", async () => {
    vi.mocked(APIClient.maintenance.library).mockRejectedValue(
      new ApiError(403, "admin role required", "admin_required"),
    );
    renderPage();

    expect((await screen.findByRole("alert")).textContent).toBe(
      "Admin access is required to view the movie library.",
    );
  });
});

describe("AdminMaintenancePage correction", () => {
  it("points a movie at the TMDB result the admin picks", async () => {
    renderPage();
    const dialog = await openCorrection("Wrong Pick");
    const save = within(dialog).getByRole("button", { name: "Save correction" });
    expect(save.hasAttribute("disabled")).toBe(true);

    fireEvent.change(within(dialog).getByRole("searchbox", { name: "TMDB title" }), {
      target: { value: "right pick" },
    });
    fireEvent.click(within(dialog).getByRole("button", { name: "Search TMDB" }));
    fireEvent.click(await within(dialog).findByRole("radio", { name: /Right Pick/ }));
    fireEvent.click(save);

    await waitFor(() =>
      expect(APIClient.maintenance.correctMovie).toHaveBeenCalledWith(1, {
        tmdbId: 200,
        title: "Right Pick",
        year: 1999,
      }),
    );
    await waitFor(() =>
      expect(screen.queryByRole("dialog", { name: "Correct movie" })).toBeNull(),
    );
    expect(toast.success).toHaveBeenCalledWith("Wrong Pick corrected");
  });

  it("does not offer the movie's current TMDB entry as a correction", async () => {
    vi.mocked(APIClient.tmdb.search).mockResolvedValue([
      { ...rightPick, id: 100, title: "Wrong Pick" },
    ]);
    renderPage();
    const dialog = await openCorrection("Wrong Pick");

    fireEvent.change(within(dialog).getByRole("searchbox", { name: "TMDB title" }), {
      target: { value: "wrong" },
    });
    fireEvent.click(within(dialog).getByRole("button", { name: "Search TMDB" }));

    const current = await within(dialog).findByRole("radio", { name: /Wrong Pick/ });
    expect(current.hasAttribute("disabled")).toBe(true);
  });

  it("shows the watched date only for a watched movie", async () => {
    renderPage();
    const stash = await openCorrection("Wrong Pick");
    expect(within(stash).queryByLabelText("Watched date and time")).toBeNull();
    fireEvent.click(within(stash).getByRole("button", { name: "Cancel" }));
    await waitFor(() =>
      expect(screen.queryByRole("dialog", { name: "Correct movie" })).toBeNull(),
    );

    const watched = await openCorrection("Alien");
    expect(within(watched).getByLabelText("Watched date and time")).not.toBeNull();
  });

  it("sends a changed watched date as ISO and nothing else", () =>
    withTZ("UTC", async () => {
      renderPage();
      const dialog = await openCorrection("Alien");

      fireEvent.change(within(dialog).getByLabelText("Watched date and time"), {
        target: { value: "2026-02-08T16:45" },
      });
      fireEvent.click(within(dialog).getByRole("button", { name: "Save correction" }));

      await waitFor(() =>
        expect(APIClient.maintenance.correctMovie).toHaveBeenCalledWith(2, {
          watchedAt: "2026-02-08T16:45:00.000Z",
        }),
      );
    }));

  it("omits an unchanged watched date to preserve its exact timestamp", async () => {
    renderPage();
    const dialog = await openCorrection("Alien");

    fireEvent.change(within(dialog).getByRole("searchbox", { name: "TMDB title" }), {
      target: { value: "right pick" },
    });
    fireEvent.click(within(dialog).getByRole("button", { name: "Search TMDB" }));
    fireEvent.click(await within(dialog).findByRole("radio", { name: /Right Pick/ }));
    fireEvent.click(within(dialog).getByRole("button", { name: "Save correction" }));

    await waitFor(() =>
      expect(APIClient.maintenance.correctMovie).toHaveBeenCalledWith(2, {
        tmdbId: 200,
        title: "Right Pick",
        year: 1999,
      }),
    );
  });

  it("requires a valid watched date", async () => {
    renderPage();
    const dialog = await openCorrection("Alien");
    const input = within(dialog).getByLabelText("Watched date and time");

    fireEvent.change(input, { target: { value: "" } });
    fireEvent.blur(input);

    expect(input.getAttribute("aria-invalid")).toBe("true");
    expect(within(dialog).getByRole("alert").textContent).toBe(
      "Watched date and time is required.",
    );
    expect(
      within(dialog).getByRole("button", { name: "Save correction" }).hasAttribute("disabled"),
    ).toBe(true);
  });

  it("rejects a watched time that does not exist during spring-forward", () =>
    withTZ("Europe/Berlin", async () => {
      vi.mocked(APIClient.maintenance.library).mockResolvedValue([
        { ...library[1], watchedAt: "2026-03-28T01:30:37.123Z" },
      ]);
      renderPage();
      const dialog = await openCorrection("Alien");
      const input = within(dialog).getByLabelText("Watched date and time");

      fireEvent.change(input, { target: { value: "2026-03-29T02:30" } });
      fireEvent.blur(input);

      expect(within(dialog).getByRole("alert").textContent).toBe(
        "Enter a valid watched date and time.",
      );
      expect(
        within(dialog).getByRole("button", { name: "Save correction" }).hasAttribute("disabled"),
      ).toBe(true);
    }));

  it("keeps the dialog open and explains a locked acquisition", async () => {
    vi.mocked(APIClient.maintenance.correctMovie).mockRejectedValue(
      new ApiError(409, "abandon the acquisition first", "acquisition_locked"),
    );
    renderPage();
    const dialog = await openCorrection("Wrong Pick");

    fireEvent.change(within(dialog).getByRole("searchbox", { name: "TMDB title" }), {
      target: { value: "right pick" },
    });
    fireEvent.click(within(dialog).getByRole("button", { name: "Search TMDB" }));
    fireEvent.click(await within(dialog).findByRole("radio", { name: /Right Pick/ }));
    fireEvent.click(within(dialog).getByRole("button", { name: "Save correction" }));

    expect((await within(dialog).findByRole("alert")).textContent).toBe(
      "Radarr already holds this movie. Abandon its acquisition first.",
    );
    expect(screen.getByRole("dialog", { name: "Correct movie" })).not.toBeNull();
  });
});
