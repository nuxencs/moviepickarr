import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { StrictMode } from "react";
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

function renderPicker(strict = false) {
  const client = new QueryClient({ defaultOptions: { mutations: { retry: false } } });
  const invalidate = vi.spyOn(client, "invalidateQueries");
  const picker = (
    <QueryClientProvider client={client}>
      <RadarrReleasePicker acquisition={ACQUISITION} onClose={() => {}} />
    </QueryClientProvider>
  );
  render(strict ? <StrictMode>{picker}</StrictMode> : picker);
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
  it("searches immediately and refreshes acquisition state after success", async () => {
    api.search.mockResolvedValue([]);
    const invalidate = renderPicker();

    await waitFor(() => expect(api.search).toHaveBeenCalledOnce());
    expect(await screen.findByText("No mapped releases were found.")).toBeTruthy();
    await expectAcquisitionInvalidations(invalidate);
  });

  it("starts the automatic search once per picker mount under StrictMode", async () => {
    api.search.mockResolvedValue([]);
    renderPicker(true);

    await waitFor(() => expect(api.search).toHaveBeenCalledOnce());
    expect(await screen.findByText("No mapped releases were found.")).toBeTruthy();
  });

  it("refreshes acquisition state after an immediate search fails", async () => {
    api.search.mockRejectedValue(
      new IntegrationProblem(409, "conflict", "Radarr already has active work"),
    );
    const invalidate = renderPicker();

    expect((await screen.findByRole("alert")).textContent).toBe(
      "Radarr already has active work",
    );
    await expectAcquisitionInvalidations(invalidate);
  });

  it("keeps a fixed search-again action after the initial search", async () => {
    api.search.mockResolvedValue([]);
    renderPicker();

    await waitFor(() => expect(api.search).toHaveBeenCalledOnce());
    const searchAgain = await screen.findByRole("button", { name: "Search releases again" });
    expect(searchAgain.textContent).toBe("");
    fireEvent.click(searchAgain);

    await waitFor(() => expect(api.search).toHaveBeenCalledTimes(2));
  });

  it("compacts release facts and calls the accessible download action", async () => {
    api.search.mockResolvedValue([{
      id: "arrival-1080p",
      title: "Arrival.2016.1080p.BluRay",
      quality: "Bluray-1080p",
      size: 12_884_901_888,
      ageHours: 10_000,
      peers: 0,
      protocol: "torrent",
      indexer: "Prowlarr",
      customFormats: ["Preferred group", "Original language"],
      customFormatScore: 1450,
      mapped: true,
      grabAllowed: true,
    }]);
    api.grab.mockResolvedValue({ ...ACQUISITION, status: "waiting_for_radarr" });
    renderPicker();

    expect(await screen.findByText("1.1 years")).toBeTruthy();
    expect(screen.getByText("0 peers")).toBeTruthy();
    const releaseList = screen.getByRole("list", { name: "Approved releases" });
    expect(within(releaseList).getByText("Bluray-1080p")).toBeTruthy();
    expect(within(releaseList).getByText("1450")).toBeTruthy();
    const score = within(releaseList).getByRole("button", { name: "Score 1450. Show applied custom formats" });
    expect(screen.queryByRole("tooltip")).toBeNull();
    fireEvent.focus(score);
    expect(within(screen.getByRole("tooltip")).getByText("Preferred group · Original language")).toBeTruthy();
    fireEvent.keyDown(score, { key: "Escape" });
    expect(screen.queryByRole("tooltip")).toBeNull();
    fireEvent.click(score);
    const pinnedTooltip = screen.getByRole("tooltip");
    fireEvent.pointerDown(pinnedTooltip);
    expect(screen.getByRole("tooltip")).toBe(pinnedTooltip);
    fireEvent.pointerDown(document.body);
    expect(screen.queryByRole("tooltip")).toBeNull();
    const download = screen.getByRole("button", { name: "Download Arrival.2016.1080p.BluRay" });
    expect(download.textContent).toBe("");

    fireEvent.click(download);

    await waitFor(() => expect(api.grab).toHaveBeenCalledWith(42, "arrival-1080p", false));
  });

  it("keeps a long custom-format tooltip inside a short viewport", async () => {
    api.search.mockResolvedValue([{
      id: "arrival-viewport",
      title: "Arrival.2016.1080p.BluRay",
      quality: "Bluray-1080p",
      customFormats: ["Preferred group", "Original language"],
      customFormatScore: 1450,
      mapped: true,
      grabAllowed: true,
    }]);
    const innerHeight = vi.spyOn(window, "innerHeight", "get").mockReturnValue(100);
    const rectangles = vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockImplementation(function (this: HTMLElement) {
      if (this.classList.contains("radarr-release__score")) {
        return { top: 40, right: 240, bottom: 50, left: 200, width: 40, height: 10, x: 200, y: 40, toJSON: () => ({}) } as DOMRect;
      }
      if (this.classList.contains("radarr-release__formats-tooltip")) {
        return { top: 0, right: 240, bottom: 120, left: 0, width: 240, height: 120, x: 0, y: 0, toJSON: () => ({}) } as DOMRect;
      }
      return { top: 0, right: 0, bottom: 0, left: 0, width: 0, height: 0, x: 0, y: 0, toJSON: () => ({}) } as DOMRect;
    });

    try {
      renderPicker();
      const score = await screen.findByRole("button", { name: "Score 1450. Show applied custom formats" });
      fireEvent.focus(score);

      await waitFor(() => expect(screen.getByRole("tooltip").style.top).toBe("8px"));
    } finally {
      rectangles.mockRestore();
      innerHeight.mockRestore();
    }
  });

  it("hides healthy peer counts and keeps the rejected-release confirmation", async () => {
    api.search.mockResolvedValue([{
      id: "arrival-rejected",
      title: "Arrival.2016.2160p.BluRay",
      quality: "Bluray-2160p",
      ageHours: 48,
      peers: 80,
      rejected: true,
      rejections: ["Release is larger than the preferred size."],
      mapped: true,
      grabAllowed: true,
    }]);
    renderPicker();

    const rejected = await screen.findByRole("button", { name: "Rejected releases" });
    expect(within(rejected).getByText("1")).toBeTruthy();
    expect(screen.queryByText("80 peers")).toBeNull();
    expect(screen.queryByRole("button", { name: /Show applied custom formats/ })).toBeNull();
    fireEvent.click(rejected);
    fireEvent.click(screen.getByRole("button", {
      name: "Review Arrival.2016.2160p.BluRay before download",
    }));

    expect(await screen.findByRole("dialog", { name: "Grab rejected release?" })).toBeTruthy();
    expect(screen.getByText("Release is larger than the preferred size.")).toBeTruthy();
  });
});
