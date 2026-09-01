import { afterEach, describe, expect, it, vi } from "vitest";

import { ApiError, HttpClient } from "@/api/APIClient";

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("HttpClient problem responses", () => {
  it("keeps the machine-readable problem title on ApiError", async () => {
    const fetch = vi.fn().mockResolvedValue(new Response(JSON.stringify({
      title: "turn_handoff_confirmation_required",
      status: 409,
      detail: "Confirm the turn handoff.",
    }), {
      status: 409,
      headers: { "Content-Type": "application/problem+json" },
    }));
    vi.stubGlobal("window", { fetch });

    const error = await HttpClient("api/v1/members/7/role", {
      method: "PATCH",
      body: { role: "guest" },
    }).catch((reason: unknown) => reason);

    expect(error).toBeInstanceOf(ApiError);
    expect(error).toMatchObject({
      status: 409,
      code: "turn_handoff_confirmation_required",
      message: "Confirm the turn handoff.",
    });
  });
});
