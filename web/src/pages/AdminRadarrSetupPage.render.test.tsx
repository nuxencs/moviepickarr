import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { AdminRadarrSetupPage } from "@/pages/AdminRadarrSetupPage";

const api = vi.hoisted(() => ({
  listInstances: vi.fn(),
  listPresets: vi.fn(),
}));

vi.mock("@/api/radarr", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api/radarr")>()),
  listRadarrInstances: api.listInstances,
  listRadarrPresets: api.listPresets,
}));

function renderPage() {
  const client = new QueryClient({
    defaultOptions: { mutations: { retry: false }, queries: { retry: false } },
  });
  render(
    <QueryClientProvider client={client}>
      <AdminRadarrSetupPage />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  api.listInstances.mockReset();
  api.listPresets.mockReset();
  api.listInstances.mockResolvedValue([{
    id: 3,
    name: "Radarr 1080p",
    url: "https://radarr.example.test",
    state: "connected",
    lastTestedAt: "2026-08-01T12:00:00Z",
  }]);
  api.listPresets.mockResolvedValue([{
    id: 7,
    name: "Movies 1080p",
    instanceId: 3,
    rootFolderPath: "/movies",
    qualityProfileId: 8,
    qualityProfileName: "HD-1080p",
    minimumAvailability: "released",
    mode: "manual",
    valid: true,
  }]);
});

describe("Radarr setup tree", () => {
  it("groups presets under their instance without a repeated page heading or test timestamp", async () => {
    renderPage();

    expect(await screen.findByText("Radarr 1080p")).toBeTruthy();
    const presets = screen.getByRole("list", { name: "Radarr 1080p presets" });
    expect(within(presets).getByText("Movies 1080p")).toBeTruthy();
    expect(screen.queryByRole("heading", { name: "Setup" })).toBeNull();
    expect(screen.queryByText(/Tested/)).toBeNull();
    expect(screen.getByText("Connection verified on save")).toBeTruthy();
  });

  it("uses accessible icon controls and danger styling for archive", async () => {
    renderPage();

    const edit = await screen.findByRole("button", { name: "Edit Radarr 1080p" });
    const archive = screen.getByRole("button", { name: "Archive Radarr 1080p" });
    expect(edit.textContent).toBe("");
    expect(archive.textContent).toBe("");
    expect(archive.classList.contains("iconbtn--danger")).toBe(true);
  });
});
