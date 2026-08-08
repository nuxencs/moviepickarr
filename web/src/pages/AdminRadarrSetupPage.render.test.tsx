import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { AdminRadarrSetupPage } from "@/pages/AdminRadarrSetupPage";

const api = vi.hoisted(() => ({
  listInstances: vi.fn(),
  listPresets: vi.fn(),
  removeInstance: vi.fn(),
  removePreset: vi.fn(),
}));
const notifications = vi.hoisted(() => ({ error: vi.fn(), info: vi.fn(), success: vi.fn() }));

vi.mock("@/api/radarr", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api/radarr")>()),
  listRadarrInstances: api.listInstances,
  listRadarrPresets: api.listPresets,
  removeRadarrInstance: api.removeInstance,
  removeRadarrPreset: api.removePreset,
}));
vi.mock("@/components/ui/toast-api", () => ({ toast: notifications }));

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
  api.removeInstance.mockReset();
  api.removePreset.mockReset();
  notifications.error.mockReset();
  notifications.info.mockReset();
  notifications.success.mockReset();
  api.removeInstance.mockResolvedValue({ outcome: "archived" });
  api.removePreset.mockResolvedValue({ outcome: "deleted" });
  api.listInstances.mockResolvedValue([{
    id: 3,
    name: "Radarr 1080p",
    url: "https://radarr.example.test",
    state: "connected",
    lastTestedAt: "2026-08-01T12:00:00Z",
    used: true,
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
    used: false,
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

  it("uses the shared disclosure for archived setup", async () => {
    api.listInstances.mockResolvedValue([{
      id: 11,
      name: "Old Radarr",
      url: "https://old-radarr.example.test",
      state: "connected",
      archivedAt: "2026-07-01T12:00:00Z",
    }]);
    api.listPresets.mockResolvedValue([{
      id: 12,
      name: "Old movies",
      instanceId: 11,
      rootFolderPath: "/old-movies",
      qualityProfileId: 4,
      mode: "manual",
      valid: true,
      archivedAt: "2026-07-01T12:00:00Z",
    }]);
    renderPage();

    const trigger = await screen.findByRole("button", { name: "Archived setup" });
    expect(within(trigger).getByText("2")).toBeTruthy();
    expect(trigger.getAttribute("aria-expanded")).toBe("false");
    expect(screen.queryByRole("region", { name: "Archived setup" })).toBeNull();

    fireEvent.click(trigger);

    expect(trigger.getAttribute("aria-expanded")).toBe("true");
    const archive = screen.getByRole("region", { name: "Archived setup" });
    expect(within(archive).getByText("Old Radarr")).toBeTruthy();
    expect(within(archive).getByText("Old movies")).toBeTruthy();
  });

  it("previews archive for used setup and deletion for unused setup", async () => {
    renderPage();

    const edit = await screen.findByRole("button", { name: "Edit Radarr 1080p" });
    const archive = screen.getByRole("button", { name: "Archive Radarr 1080p" });
    const removePreset = screen.getByRole("button", { name: "Delete Movies 1080p" });
    expect(edit.textContent).toBe("");
    expect(archive.textContent).toBe("");
    expect(removePreset.textContent).toBe("");
    expect(archive.classList.contains("iconbtn--danger")).toBe(true);
    expect(removePreset.classList.contains("iconbtn--danger")).toBe(true);

    fireEvent.click(removePreset);
    expect(screen.getByRole("heading", { name: "Delete Movies 1080p?" })).toBeTruthy();
    expect(screen.getByText("This preset has never been used. It will be deleted permanently.")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Delete preset" }));

    await waitFor(() => expect(api.removePreset).toHaveBeenCalledWith(7));
    expect(notifications.success).toHaveBeenCalledWith("Movies 1080p deleted");
  });
});
