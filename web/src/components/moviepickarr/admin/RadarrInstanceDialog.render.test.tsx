import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { IntegrationProblem } from "@/api/integrations";
import { RadarrKeys, type RadarrInstance } from "@/api/radarr";

import { RadarrInstanceDialog } from "@/components/moviepickarr/admin/RadarrInstanceDialog";

const api = vi.hoisted(() => ({
  create: vi.fn(),
  update: vi.fn(),
}));

vi.mock("@/api/radarr", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api/radarr")>()),
  createRadarrInstance: api.create,
  updateRadarrInstance: api.update,
}));

const INSTANCE: RadarrInstance = {
  id: 3,
  name: "Movies 1080p",
  url: "https://radarr.example.test",
  apiKeyConfigured: true,
  revision: 4,
};

beforeEach(() => {
  api.create.mockReset();
  api.update.mockReset();
});

describe("Radarr instance concurrent edits", () => {
  it("reloads the latest setup record after a stale revision", async () => {
    api.update.mockRejectedValue(
      new IntegrationProblem(
        409,
        "stale_revision",
        "another admin changed these settings",
      ),
    );
    const client = new QueryClient({ defaultOptions: { mutations: { retry: false } } });
    const invalidate = vi.spyOn(client, "invalidateQueries");
    const onClose = vi.fn();
    render(
      <QueryClientProvider client={client}>
        <RadarrInstanceDialog instance={INSTANCE} onClose={onClose} onSaved={() => {}} />
      </QueryClientProvider>,
    );

    fireEvent.click(screen.getByRole("button", { name: "Test and save" }));
    const reload = await screen.findByRole("button", { name: "Reload latest" });
    expect(screen.getByRole("alert").textContent).toContain(
      "Another Admin changed this instance.",
    );

    fireEvent.click(reload);

    await waitFor(() => {
      expect(invalidate).toHaveBeenCalledWith({ queryKey: RadarrKeys.instances() });
      expect(onClose).toHaveBeenCalledOnce();
    });
  });

  it("requires the API key again before saving a different endpoint", () => {
    const client = new QueryClient({ defaultOptions: { mutations: { retry: false } } });
    render(
      <QueryClientProvider client={client}>
        <RadarrInstanceDialog instance={INSTANCE} onClose={() => {}} onSaved={() => {}} />
      </QueryClientProvider>,
    );

    fireEvent.change(screen.getByRole("textbox", { name: "Radarr URL" }), {
      target: { value: "http://capture.example.test" },
    });

    expect(screen.getByText(/URL scheme or host changed/)).toBeTruthy();
    expect(
      (screen.getByRole("button", { name: "Test and save" }) as HTMLButtonElement).disabled,
    ).toBe(true);
    expect(api.update).not.toHaveBeenCalled();
  });
});
