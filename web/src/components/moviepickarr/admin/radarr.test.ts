import { describe, expect, it } from "vitest";

import {
  acquisitionIsOpen,
  formatBytes,
  radarrReasonLabel,
  radarrStatusLabel,
  targetName,
} from "@/components/moviepickarr/admin/radarr";

describe("Radarr Admin labels", () => {
  it("keeps only terminal acquisitions out of the attention register", () => {
    expect(acquisitionIsOpen({ id: 1, status: "needs_release", mutationState: "idle" })).toBe(true);
    expect(acquisitionIsOpen({ id: 2, status: "downloaded", mutationState: "idle" })).toBe(false);
    expect(acquisitionIsOpen({ id: 3, status: "abandoned", mutationState: "idle" })).toBe(false);
    expect(acquisitionIsOpen({ id: 4, status: "canceled", mutationState: "idle" })).toBe(false);
  });

  it("provides readable labels for known and future remote states", () => {
    expect(radarrStatusLabel("waiting_for_radarr")).toBe("Waiting for Radarr");
    expect(radarrStatusLabel("new_remote_state")).toBe("New remote state");
    expect(radarrReasonLabel("no_releases")).toBe("No releases were found");
  });

  it("formats optional acquisition facts without throwing", () => {
    expect(targetName()).toBe("No target selected");
    expect(targetName({ presetName: "Anime 1080p" })).toBe("Anime 1080p");
    expect(formatBytes()).toBe("Size unavailable");
    expect(formatBytes(1_073_741_824)).toBe("1.0 GB");
  });
});
