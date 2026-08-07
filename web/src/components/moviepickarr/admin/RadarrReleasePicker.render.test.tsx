import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { IntegrationProblem } from "@/api/integrations";
import { RadarrKeys, type RadarrAcquisition } from "@/api/radarr";

import { RadarrReleasePicker } from "@/components/moviepickarr/admin/RadarrReleasePicker";

const api = vi.hoisted(() => ({
  grab: vi.fn(),
  search: vi.fn(),
}));

vi.mock("@/api/radarr", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api/radarr")>()),
  grabRadarrRelease: api.grab,
  searchRadarrReleases: api.search,
}));

const ACQUISITION: RadarrAcquisition = {
  id: 42,
  title: "Arrival",
  status: "needs_release",
  mutationState: "idle",
  targetLocked: true,
};

function renderPicker() {
  const client = new QueryClient({ defaultOptions: { mutations: { retry: false } } });
  const invalidate = vi.spyOn(client, "invalidateQueries");
  render(
    <QueryClientProvider client={client}>
      <RadarrReleasePicker acquisition={ACQUISITION} onClose={() => {}} />
    </QueryClientProvider>,
  );
  return invalidate;
}

async function expectAcquisitionInvalidations(
  invalidate: ReturnType<typeof renderPicker>,
) {
  await waitFor(() => {
    expect(invalidate).toHaveBeenCalledWith({ queryKey: RadarrKeys.acquisition(42) });
    expect(invalidate).toHaveBeenCalledWith({ queryKey: RadarrKeys.acquisitions() });
    expect(invalidate).toHaveBeenCalledWith({ queryKey: RadarrKeys.attention() });
  });
}

beforeEach(() => {
  api.grab.mockReset();
  api.search.mockReset();
});

describe("Radarr release search synchronization", () => {
  it("refreshes acquisition state after a successful search", async () => {
    api.search.mockResolvedValue([]);
    const invalidate = renderPicker();

    fireEvent.click(screen.getByRole("button", { name: "Search Radarr" }));

    expect(await screen.findByText("No mapped releases were found.")).toBeTruthy();
    await expectAcquisitionInvalidations(invalidate);
  });

  it("refreshes acquisition state after a failed search", async () => {
    api.search.mockRejectedValue(
      new IntegrationProblem(409, "conflict", "Radarr already has active work"),
    );
    const invalidate = renderPicker();

    fireEvent.click(screen.getByRole("button", { name: "Search Radarr" }));

    expect((await screen.findByRole("alert")).textContent).toBe(
      "Radarr already has active work",
    );
    await expectAcquisitionInvalidations(invalidate);
  });
});
