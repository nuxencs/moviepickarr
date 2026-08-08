import { afterEach, describe, expect, it, vi } from "vitest";

import {
  abandonRadarrAcquisition,
  removeRadarrInstance,
  removeRadarrPreset,
  getRadarrAttention,
  grabRadarrRelease,
  listRadarrAcquisitions,
  reviewRadarrAbandonment,
  searchRadarrReleases,
  selectRadarrPreset,
  testRadarrWebhookDraft,
} from "@/api/radarr";

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("Radarr integration API", () => {
  it("normalizes attention counts without exposing acquisition details", async () => {
    const fetch = vi.fn().mockResolvedValue(jsonResponse({ attentionCount: 4 }));
    vi.stubGlobal("fetch", fetch);

    await expect(getRadarrAttention()).resolves.toEqual({ count: 4 });
    expect(fetch).toHaveBeenCalledWith(
      "/api/v1/integrations/radarr/attention",
      expect.objectContaining({ method: "GET", credentials: "include" }),
    );
  });

  it("accepts both plain and enveloped acquisition lists", async () => {
    const first = { id: 11, title: "Arrival", status: "needs_release" };
    const second = { id: 12, title: "Heat", status: "downloaded" };
    const fetch = vi
      .fn()
      .mockResolvedValueOnce(jsonResponse([first]))
      .mockResolvedValueOnce(jsonResponse({ items: [second] }));
    vi.stubGlobal("fetch", fetch);

    await expect(listRadarrAcquisitions()).resolves.toEqual([first]);
    await expect(listRadarrAcquisitions()).resolves.toEqual([second]);
  });

  it("returns the delete-or-archive outcome for setup removal", async () => {
    const fetch = vi
      .fn()
      .mockResolvedValueOnce(jsonResponse({ outcome: "deleted" }))
      .mockResolvedValueOnce(jsonResponse({ outcome: "archived" }));
    vi.stubGlobal("fetch", fetch);

    await expect(removeRadarrPreset(17)).resolves.toEqual({ outcome: "deleted" });
    await expect(removeRadarrInstance(4)).resolves.toEqual({ outcome: "archived" });
    expect(fetch).toHaveBeenNthCalledWith(
      1,
      "/api/v1/integrations/radarr/presets/17",
      expect.objectContaining({ method: "DELETE", credentials: "include" }),
    );
    expect(fetch).toHaveBeenNthCalledWith(
      2,
      "/api/v1/integrations/radarr/instances/4",
      expect.objectContaining({ method: "DELETE", credentials: "include" }),
    );
  });

  it("sends target and release choices to their acquisition actions", async () => {
    const fetch = vi
      .fn()
      .mockResolvedValueOnce(jsonResponse({ id: 18, status: "needs_release" }))
      .mockResolvedValueOnce(jsonResponse({ id: 18, status: "downloading" }));
    vi.stubGlobal("fetch", fetch);

    await selectRadarrPreset(18, 44);
    await grabRadarrRelease(18, "rr/result with spaces", true);

    expect(fetch).toHaveBeenNthCalledWith(
      1,
      "/api/v1/integrations/radarr/acquisitions/18/preset",
      expect.objectContaining({
        method: "PUT",
        body: JSON.stringify({ presetId: 44 }),
      }),
    );
    expect(fetch).toHaveBeenNthCalledWith(
      2,
      "/api/v1/integrations/radarr/acquisitions/18/releases/rr%2Fresult%20with%20spaces/grab",
      expect.objectContaining({
        method: "POST",
        body: JSON.stringify({ override: true }),
      }),
    );
  });

  it("requests a live read-only review before abandonment", async () => {
    const review = {
      acquisition: { id: 18, title: "Arrival", status: "downloading", activeQueue: true },
      activity: "active",
    };
    const fetch = vi.fn().mockResolvedValue(jsonResponse(review));
    vi.stubGlobal("fetch", fetch);

    await expect(reviewRadarrAbandonment(18)).resolves.toEqual(review);
    expect(fetch).toHaveBeenCalledWith(
      "/api/v1/integrations/radarr/acquisitions/18/abandon/review",
      expect.objectContaining({ method: "POST", credentials: "include" }),
    );
  });

  it("acknowledges the activity warning shown before abandonment", async () => {
    const fetch = vi.fn().mockResolvedValue(jsonResponse({ id: 18, status: "abandoned" }));
    vi.stubGlobal("fetch", fetch);

    await abandonRadarrAcquisition(18, "No longer needed", "active");
    expect(fetch).toHaveBeenCalledWith(
      "/api/v1/integrations/radarr/acquisitions/18/abandon",
      expect.objectContaining({
        method: "POST",
        body: JSON.stringify({ reason: "No longer needed", acknowledgedActivity: "active" }),
      }),
    );
  });

  it("keeps only sanitized release fields in the browser cache", async () => {
    const fetch = vi.fn().mockResolvedValue(jsonResponse({
      items: [{
        resultId: "rr_safe_handle",
        title: "Arrival.2016.1080p",
        quality: { name: "Bluray-1080p" },
        seeders: 18,
        customFormats: ["Preferred group", "Original language", { name: "Not accepted" }],
        customFormatScore: 1450,
        rejected: true,
        rejectionReasons: ["Custom format score is below zero"],
        downloadUrl: "https://indexer.example/private-token",
        magnetUrl: "magnet:?xt=private",
      }],
    }));
    vi.stubGlobal("fetch", fetch);

    const releases = await searchRadarrReleases(18);

    expect(releases[0]).toMatchObject({
      id: "rr_safe_handle",
      title: "Arrival.2016.1080p",
      quality: "Bluray-1080p",
      peers: 18,
      customFormats: ["Preferred group", "Original language"],
      customFormatScore: 1450,
      rejected: true,
      rejections: ["Custom format score is below zero"],
      grabAllowed: true,
    });
    expect(releases[0]).not.toHaveProperty("downloadUrl");
    expect(releases[0]).not.toHaveProperty("magnetUrl");
  });

  it("tests unsaved Discord webhook drafts before they are enabled", async () => {
    const draft = {
      name: "Acquisition attention",
      format: "discord" as const,
      url: "https://discord.com/api/webhooks/redacted",
      enabled: false,
      reasons: ["release_required"],
      roleMention: "123456789",
    };
    const fetch = vi.fn().mockResolvedValue(jsonResponse({ verified: true }));
    vi.stubGlobal("fetch", fetch);

    await expect(testRadarrWebhookDraft(draft)).resolves.toEqual({ verified: true });
    expect(fetch).toHaveBeenCalledWith(
      "/api/v1/integrations/radarr/webhooks/test",
      expect.objectContaining({ method: "POST", body: JSON.stringify(draft) }),
    );
  });
});
