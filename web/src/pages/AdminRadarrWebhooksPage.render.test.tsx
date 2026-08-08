import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type { RadarrWebhook } from "@/api/radarr";

import { AdminRadarrWebhooksPage } from "@/pages/AdminRadarrWebhooksPage";

const api = vi.hoisted(() => ({
  archive: vi.fn(),
  list: vi.fn(),
  test: vi.fn(),
  update: vi.fn(),
}));
const notifications = vi.hoisted(() => ({
  error: vi.fn(),
  info: vi.fn(),
  success: vi.fn(),
}));

vi.mock("@/api/radarr", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api/radarr")>()),
  archiveRadarrWebhook: api.archive,
  listRadarrWebhooks: api.list,
  testRadarrWebhook: api.test,
  updateRadarrWebhook: api.update,
}));
vi.mock("@/components/ui/toast-api", () => ({ toast: notifications }));

const DESTINATION: RadarrWebhook = {
  id: 8,
  name: "Movie night Discord",
  format: "discord",
  enabled: false,
  verified: true,
  reasons: ["preset_required", "release_required"],
  roleMention: "1234567890",
  revision: 4,
};

function renderPage() {
  const client = new QueryClient({
    defaultOptions: { mutations: { retry: false }, queries: { retry: false } },
  });
  render(
    <QueryClientProvider client={client}>
      <AdminRadarrWebhooksPage />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  api.archive.mockReset();
  api.list.mockReset();
  api.test.mockReset();
  api.update.mockReset();
  notifications.error.mockReset();
  notifications.info.mockReset();
  notifications.success.mockReset();
  api.list.mockResolvedValue([DESTINATION]);
});

describe("Radarr webhook destination controls", () => {
  it("uses the shared disclosure for archived destinations", async () => {
    api.list.mockResolvedValue([{
      ...DESTINATION,
      id: 12,
      name: "Old Discord",
      archivedAt: "2026-07-01T12:00:00Z",
    }]);
    renderPage();

    const trigger = await screen.findByRole("button", { name: "Archived destinations" });
    expect(within(trigger).getByText("1")).toBeTruthy();
    expect(trigger.getAttribute("aria-expanded")).toBe("false");
    fireEvent.click(trigger);

    const archive = screen.getByRole("region", { name: "Archived destinations" });
    expect(within(archive).getByText("Old Discord")).toBeTruthy();
  });

  it("enables a verified destination without sending its write-only URL", async () => {
    api.list.mockReset();
    api.list
      .mockResolvedValueOnce([DESTINATION])
      .mockResolvedValue([{ ...DESTINATION, enabled: true, revision: 5 }]);
    api.update.mockResolvedValue({ ...DESTINATION, enabled: true, revision: 5 });
    renderPage();

    const toggle = await screen.findByRole("switch", { name: "Enable Movie night Discord" });
    fireEvent.click(toggle);

    await waitFor(() => expect(api.update).toHaveBeenCalledOnce());
    expect(api.update).toHaveBeenCalledWith(8, {
      name: "Movie night Discord",
      format: "discord",
      enabled: true,
      reasons: ["preset_required", "release_required"],
      roleMention: "1234567890",
      revision: 4,
    });
    expect(await screen.findByRole("switch", { name: "Disable Movie night Discord" })).toBeTruthy();
    expect(notifications.success).toHaveBeenCalledWith("Movie night Discord enabled");
    expect(document.querySelector(".radarr-action-feedback")).toBeNull();
  });

  it("reports row action failures without moving the destination list", async () => {
    api.test.mockRejectedValue(new Error("network unavailable"));
    renderPage();

    fireEvent.click(await screen.findByRole("button", { name: "Test Movie night Discord" }));

    await waitFor(() => expect(notifications.error).toHaveBeenCalledWith("The test delivery failed."));
    expect(document.querySelector(".radarr-action-feedback")).toBeNull();
    expect(screen.getByRole("list", { name: "Radarr webhook destinations" })).toBeTruthy();
  });

  it("requires a successful test before an inactive destination can be enabled", async () => {
    api.list.mockResolvedValue([{ ...DESTINATION, verified: false, lastTestedAt: "2026-08-06T12:00:00Z" }]);
    renderPage();

    const toggle = await screen.findByRole("switch", { name: "Enable Movie night Discord" });
    expect((toggle as HTMLInputElement).disabled).toBe(true);
    expect(screen.queryByText("Test required")).toBeNull();
    expect(screen.getByRole("tooltip").textContent).toBe("Test this destination before enabling it.");
    expect(screen.queryByText(/Tested/)).toBeNull();
    expect(screen.getByText(/2 reason filters/)).toBeTruthy();
    expect(toggle.closest("li")?.firstElementChild).toBe(toggle.closest("label"));

    const switchCell = toggle.closest("label");
    const tooltip = screen.getByRole("tooltip");
    expect(switchCell?.tabIndex).toBe(0);
    expect(switchCell?.getAttribute("aria-describedby")).toBe(tooltip.id);
    expect(tooltip.textContent).toBe("Test this destination before enabling it.");
    expect(toggle.closest("li")?.firstElementChild).toBe(switchCell);
  });

  it("uses compact icon actions and marks archive as destructive", async () => {
    renderPage();

    const row = (await screen.findByRole("switch", { name: "Enable Movie night Discord" })).closest("li");
    expect(row).toBeTruthy();
    const actions = [
      within(row!).getByRole("button", { name: "Test Movie night Discord" }),
      within(row!).getByRole("button", { name: "Edit Movie night Discord" }),
      within(row!).getByRole("button", { name: "Archive Movie night Discord" }),
    ];
    expect(actions.every((button) => button.textContent === "")).toBe(true);
    expect(actions[2].classList.contains("iconbtn--danger")).toBe(true);
  });

  it("shows toggle progress only on the destination being updated", async () => {
    api.list.mockResolvedValue([
      DESTINATION,
      { ...DESTINATION, id: 9, name: "Generic automation", format: "generic" },
    ]);
    api.update.mockReturnValue(new Promise<RadarrWebhook>(() => {}));
    renderPage();

    const first = await screen.findByRole("switch", { name: "Enable Movie night Discord" });
    const second = screen.getByRole("switch", { name: "Enable Generic automation" });
    fireEvent.click(first);

    await waitFor(() => expect(api.update).toHaveBeenCalledOnce());
    expect(first.closest("label")?.getAttribute("data-pending")).toBe("true");
    expect(second.closest("label")?.hasAttribute("data-pending")).toBe(false);
  });

  it("shows loading only on the destination being tested", async () => {
    api.list.mockResolvedValue([
      DESTINATION,
      { ...DESTINATION, id: 9, name: "Generic automation", format: "generic" },
    ]);
    api.test.mockReturnValue(new Promise<RadarrWebhook>(() => {}));
    renderPage();

    fireEvent.click(await screen.findByRole("button", { name: "Test Movie night Discord" }));

    await waitFor(() => expect(screen.getByRole("button", { name: "Test Movie night Discord" }).querySelector(".mg-spin")).toBeTruthy());
    expect(screen.getByRole("button", { name: "Test Generic automation" }).querySelector(".mg-spin")).toBeNull();
    expect((screen.getByRole("switch", { name: "Enable Movie night Discord" }) as HTMLInputElement).disabled).toBe(true);
    expect((screen.getByRole("button", { name: "Edit Movie night Discord" }) as HTMLButtonElement).disabled).toBe(true);
    expect((screen.getByRole("button", { name: "Archive Movie night Discord" }) as HTMLButtonElement).disabled).toBe(true);
    expect((screen.getByRole("switch", { name: "Enable Generic automation" }) as HTMLInputElement).disabled).toBe(false);
    expect((screen.getByRole("button", { name: "Edit Generic automation" }) as HTMLButtonElement).disabled).toBe(false);
    await waitFor(() => expect(api.test).toHaveBeenCalledWith(8));
  });

  it("uses icon actions and marks archive as destructive", async () => {
    renderPage();

    const archive = await screen.findByRole("button", { name: "Archive Movie night Discord" });
    expect(archive.textContent).toBe("");
    expect(archive.classList.contains("iconbtn--danger")).toBe(true);
    expect(screen.getByRole("button", { name: "Test Movie night Discord" }).textContent).toBe("");
    expect(screen.getByRole("button", { name: "Edit Movie night Discord" }).textContent).toBe("");
  });
});
