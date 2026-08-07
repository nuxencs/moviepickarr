import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type { ReactNode } from "react";

import { AdminRadarrAcquisitionPage } from "@/pages/AdminRadarrAcquisitionPage";

const api = vi.hoisted(() => ({
  getAcquisition: vi.fn(),
  listPresets: vi.fn(),
  selectPreset: vi.fn(),
  retryAcquisition: vi.fn(),
  reviewAbandonment: vi.fn(),
  abandonAcquisition: vi.fn(),
}));

vi.mock("@tanstack/react-router", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@tanstack/react-router")>()),
  Link: ({ children, className }: { children: ReactNode; className?: string }) => (
    <a href="#acquisitions" className={className}>{children}</a>
  ),
  useParams: () => ({ acquisitionID: "42" }),
}));

vi.mock("@/api/radarr", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api/radarr")>()),
  getRadarrAcquisition: api.getAcquisition,
  listRadarrPresets: api.listPresets,
  selectRadarrPreset: api.selectPreset,
  retryRadarrAcquisition: api.retryAcquisition,
  reviewRadarrAbandonment: api.reviewAbandonment,
  abandonRadarrAcquisition: api.abandonAcquisition,
}));

window.scrollTo = (() => {}) as typeof window.scrollTo;

function renderPage() {
  const client = new QueryClient({
    defaultOptions: { mutations: { retry: false }, queries: { retry: false } },
  });
  render(
    <QueryClientProvider client={client}>
      <AdminRadarrAcquisitionPage />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  api.getAcquisition.mockReset();
  api.listPresets.mockReset();
  api.selectPreset.mockReset();
  api.retryAcquisition.mockReset();
  api.reviewAbandonment.mockReset();
  api.abandonAcquisition.mockReset();
  api.getAcquisition.mockResolvedValue({
    id: 42,
    title: "Arrival",
    status: "needs_preset",
    targetLocked: false,
  });
  api.listPresets.mockResolvedValue([{
    id: 7,
    name: "Movies 1080p",
    instanceId: 3,
    rootFolderPath: "/movies",
    qualityProfileId: 8,
    minimumAvailability: "released",
    mode: "manual",
    valid: true,
  }]);
});

describe("Radarr acquisition target safety", () => {
  it("checks an ambiguous unlocked add without exposing preset selection", async () => {
    const acquisition = {
      id: 42,
      title: "Arrival",
      status: "action_needed",
      actionReason: "connection_failed",
      mutationState: "adding",
      targetLocked: false,
      target: {
        presetId: 7,
        presetName: "Movies 1080p",
        instanceName: "Radarr 1080p",
        rootFolderPath: "/movies",
        qualityProfileName: "HD-1080p",
        minimumAvailability: "released",
        mode: "manual",
      },
    };
    api.getAcquisition.mockResolvedValue(acquisition);
    api.retryAcquisition.mockResolvedValue({
      ...acquisition,
      status: "needs_release",
      actionReason: "release_required",
      mutationState: "idle",
      targetLocked: true,
      radarrMovieId: 12,
    });
    renderPage();

    expect(await screen.findByRole("heading", { name: "Target" })).toBeTruthy();
    expect(screen.getByText("Radarr 1080p")).toBeTruthy();
    expect(screen.queryByRole("combobox", { name: "Acquisition preset" })).toBeNull();

    fireEvent.click(screen.getByRole("button", { name: "Check Radarr add" }));
    await waitFor(() => expect(api.retryAcquisition).toHaveBeenCalledWith("42"));
  });

  it("explains locked identity recovery without exposing the identity resolver", async () => {
    api.getAcquisition.mockResolvedValue({
      id: 42,
      title: "Arrival",
      status: "action_needed",
      actionReason: "identity_required",
      mutationState: "idle",
      targetLocked: true,
      radarrMovieId: 12,
      target: {
        presetName: "Movies 1080p",
        instanceName: "Radarr 1080p",
      },
    });
    renderPage();

    expect(await screen.findByRole("heading", { name: "Restore the Radarr movie" })).toBeTruthy();
    expect(screen.getByText(
      "Restore the exact movie in Radarr 1080p, or abandon this acquisition.",
    )).toBeTruthy();
    expect(screen.queryByRole("heading", { name: "Confirm identity" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Search Radarr" })).toBeNull();
  });

  it("checks Radarr status while a release grab outcome is unknown", async () => {
    const acquisition = {
      id: 42,
      title: "Arrival",
      status: "action_needed",
      actionReason: "connection_failed",
      mutationState: "grabbing",
      targetLocked: true,
      radarrMovieId: 12,
      target: {
        presetName: "Movies 1080p",
        instanceName: "Radarr 1080p",
      },
    };
    api.getAcquisition.mockResolvedValue(acquisition);
    api.retryAcquisition.mockResolvedValue(acquisition);
    renderPage();

    fireEvent.click(await screen.findByRole("button", { name: "Check Radarr status" }));
    await waitFor(() => expect(api.retryAcquisition).toHaveBeenCalledWith("42"));
    expect(screen.queryByRole("button", { name: "Retry Radarr action" })).toBeNull();
  });

  it("does not expose confirm or retry after a failed unlocked preview", async () => {
    api.selectPreset.mockResolvedValue({
      id: 42,
      title: "Arrival",
      status: "action_needed",
      actionReason: "connection_failed",
      targetLocked: false,
      target: {
        presetId: 7,
        presetName: "Movies 1080p",
        instanceName: "Radarr 1080p",
      },
    });
    renderPage();

    fireEvent.change(await screen.findByRole("combobox", { name: "Acquisition preset" }), {
      target: { value: "7" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Review target" }));

    await waitFor(() => expect(api.selectPreset).toHaveBeenCalledWith("42", 7));
    expect(screen.queryByRole("dialog", { name: "Review acquisition target" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Retry Radarr action" })).toBeNull();
  });

  it("shows a reason label and one distinct failure for an action-needed acquisition", async () => {
    api.getAcquisition.mockResolvedValue({
      id: 42,
      title: "Arrival",
      status: "action_needed",
      actionReason: "connection_failed",
      actionMessage: "Radarr could not complete the requested check.",
      latestFailure: "Radarr could not complete the requested check.",
      targetLocked: true,
      radarrMovieId: 12,
    });
    renderPage();

    expect(await screen.findByText("Restore the Radarr connection")).toBeTruthy();
    expect(
      screen.getAllByText("Radarr could not complete the requested check."),
    ).toHaveLength(1);
    expect(screen.getByRole("alert").textContent).toBe(
      "Radarr could not complete the requested check.",
    );
  });

  it("refreshes live Radarr work before showing the abandonment warning", async () => {
    const acquisition = {
      id: 42,
      title: "Arrival",
      status: "needs_release",
      actionReason: "release_required",
      targetLocked: true,
      radarrMovieId: 12,
      activeQueue: false,
    };
    api.getAcquisition.mockResolvedValue(acquisition);
    api.reviewAbandonment.mockResolvedValue({
      acquisition: { ...acquisition, status: "downloading", actionReason: undefined, activeQueue: true },
      activity: "active",
    });
    renderPage();

    fireEvent.click(await screen.findByRole("button", { name: "Abandon acquisition" }));
    const dialog = await screen.findByRole("dialog", { name: "Abandon acquisition?" });
    expect(await within(dialog).findByText(
      "Radarr still has active work for this movie. That work will continue after abandonment.",
    )).toBeTruthy();
    expect(api.reviewAbandonment).toHaveBeenCalledWith(42, expect.anything());
  });

  it("keeps abandonment available when current Radarr activity cannot be verified", async () => {
    const acquisition = {
      id: 42,
      title: "Arrival",
      status: "needs_release",
      actionReason: "release_required",
      targetLocked: true,
      radarrMovieId: 12,
      activeQueue: false,
    };
    api.getAcquisition.mockResolvedValue(acquisition);
    api.reviewAbandonment.mockResolvedValue({
      acquisition: {
        ...acquisition,
        status: "action_needed",
        actionReason: "connection_failed",
        latestFailure: "Radarr could not complete the requested check.",
      },
      activity: "unavailable",
    });
    api.abandonAcquisition.mockResolvedValue({
      ...acquisition,
      status: "abandoned",
      abandonmentReason: "No longer needed",
    });
    renderPage();

    fireEvent.click(await screen.findByRole("button", { name: "Abandon acquisition" }));
    const dialog = await screen.findByRole("dialog", { name: "Abandon acquisition?" });
    expect(await within(dialog).findByText(
      "Moviepickarr could not verify current Radarr activity. Work in Radarr may continue after abandonment.",
    )).toBeTruthy();
    fireEvent.change(within(dialog).getByRole("textbox", { name: "Reason" }), {
      target: { value: "No longer needed" },
    });
    fireEvent.click(within(dialog).getByRole("button", { name: "Abandon acquisition" }));

    await waitFor(() => expect(api.abandonAcquisition).toHaveBeenCalledWith(42, "No longer needed", "unavailable"));
  });

  it("shows newly active work before allowing a second abandonment submit", async () => {
    const acquisition = {
      id: 42,
      title: "Arrival",
      status: "needs_release",
      actionReason: "release_required",
      targetLocked: true,
      radarrMovieId: 12,
      activeQueue: false,
    };
    api.getAcquisition.mockResolvedValue(acquisition);
    api.reviewAbandonment
      .mockResolvedValueOnce({ acquisition, activity: "inactive" })
      .mockResolvedValueOnce({
        acquisition: { ...acquisition, status: "downloading", actionReason: undefined, activeQueue: true },
        activity: "active",
      });
    api.abandonAcquisition
      .mockRejectedValueOnce(new Error("activity changed"))
      .mockResolvedValueOnce({ ...acquisition, status: "abandoned" });
    renderPage();

    fireEvent.click(await screen.findByRole("button", { name: "Abandon acquisition" }));
    const dialog = await screen.findByRole("dialog", { name: "Abandon acquisition?" });
    fireEvent.change(await within(dialog).findByRole("textbox", { name: "Reason" }), {
      target: { value: "No longer needed" },
    });
    fireEvent.click(within(dialog).getByRole("button", { name: "Abandon acquisition" }));

    expect(await within(dialog).findByText(
      "Radarr still has active work for this movie. That work will continue after abandonment.",
    )).toBeTruthy();
    expect(api.abandonAcquisition).toHaveBeenNthCalledWith(1, 42, "No longer needed", "inactive");
    fireEvent.click(within(dialog).getByRole("button", { name: "Abandon acquisition" }));
    await waitFor(() => expect(api.abandonAcquisition).toHaveBeenNthCalledWith(
      2, 42, "No longer needed", "active",
    ));
  });
});
