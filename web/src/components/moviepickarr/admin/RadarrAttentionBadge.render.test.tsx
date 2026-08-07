import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { RadarrAttentionBadge } from "@/components/moviepickarr/admin/RadarrAttentionBadge";

const api = vi.hoisted(() => ({ attention: vi.fn(), me: vi.fn() }));

vi.mock("@/api/queries", () => ({
  MeQueryOptions: () => ({ queryKey: ["me"], queryFn: api.me }),
}));

vi.mock("@/api/radarr", () => ({
  RadarrAttentionQueryOptions: (enabled: boolean) => ({
    queryKey: ["integrations", "radarr", "attention"],
    queryFn: api.attention,
    enabled,
  }),
}));

function renderBadge() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <QueryClientProvider client={client}>
      <RadarrAttentionBadge />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  api.attention.mockReset();
  api.me.mockReset();
});

describe("Radarr attention badge", () => {
  it("shows an aggregate count to an Admin without a movie identity", async () => {
    api.me.mockResolvedValue({ role: "admin" });
    api.attention.mockResolvedValue({ count: 2 });

    renderBadge();

    expect(await screen.findByText("2")).toBeTruthy();
    expect(document.body.textContent).toContain("2 Radarr acquisitions need attention");
    expect(document.body.textContent).not.toContain("Arrival");
  });

  it("does not request or show infrastructure attention for a member", async () => {
    api.me.mockResolvedValue({ role: "member" });

    renderBadge();

    await waitFor(() => expect(api.me).toHaveBeenCalledOnce());
    expect(api.attention).not.toHaveBeenCalled();
    expect(screen.queryByText(/Radarr acquisition/)).toBeNull();
  });
});
