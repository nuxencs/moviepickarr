import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { IntegrationProblem } from "@/api/integrations";
import type { RadarrWebhook } from "@/api/radarr";

import { RadarrWebhookDialog } from "@/components/moviepickarr/admin/RadarrWebhookDialog";

const api = vi.hoisted(() => ({
  create: vi.fn(),
  testDraft: vi.fn(),
  testSaved: vi.fn(),
  update: vi.fn(),
}));

vi.mock("@/api/radarr", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api/radarr")>()),
  createRadarrWebhook: api.create,
  testRadarrWebhook: api.testSaved,
  testRadarrWebhookDraft: api.testDraft,
  updateRadarrWebhook: api.update,
}));

function renderDialog(destination?: RadarrWebhook, onSaved = vi.fn()) {
  const client = new QueryClient({ defaultOptions: { mutations: { retry: false } } });
  render(
    <QueryClientProvider client={client}>
      <RadarrWebhookDialog destination={destination} onClose={() => {}} onSaved={onSaved} />
    </QueryClientProvider>,
  );
  return onSaved;
}

beforeEach(() => {
  api.create.mockReset();
  api.testDraft.mockReset();
  api.testSaved.mockReset();
  api.update.mockReset();
});

describe("new Radarr webhook destination", () => {
  it("stays disabled after a successful draft test until saved and tested by ID", async () => {
    api.testDraft.mockResolvedValue({ verified: true });
    api.create.mockResolvedValue({
      id: 7,
      name: "Movie night Discord",
      format: "discord",
      enabled: false,
      verified: false,
      reasons: ["preset_required"],
    });
    renderDialog();

    fireEvent.change(screen.getByRole("textbox", { name: "Name" }), {
      target: { value: "Movie night Discord" },
    });
    fireEvent.change(screen.getByLabelText("Webhook URL"), {
      target: { value: "https://discord.com/api/webhooks/redacted" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Send test" }));

    expect(await screen.findByText(/Draft test delivered successfully/)).toBeTruthy();
    expect(
      (screen.getByRole("checkbox", { name: "Enable destination" }) as HTMLInputElement)
        .disabled,
    ).toBe(true);

    fireEvent.click(screen.getByRole("button", { name: "Save destination" }));
    await waitFor(() => expect(api.create).toHaveBeenCalledOnce());
    expect(api.create.mock.calls[0][0]).toMatchObject({ enabled: false });
  });

  it("links a server role mention issue to the Discord role field", async () => {
    api.create.mockRejectedValue(
      new IntegrationProblem(
        422,
        "validation_failed",
        "Radarr settings are invalid",
        [{ field: "roleMention", message: "Enter a Discord role ID or leave it empty." }],
      ),
    );
    renderDialog();

    fireEvent.change(screen.getByRole("textbox", { name: "Name" }), {
      target: { value: "Movie night Discord" },
    });
    fireEvent.change(screen.getByLabelText("Webhook URL"), {
      target: { value: "https://discord.com/api/webhooks/redacted" },
    });
    fireEvent.change(screen.getByRole("textbox", { name: /Role mention/ }), {
      target: { value: "not-a-role" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Save destination" }));

    const issue = await screen.findByText("Enter a Discord role ID or leave it empty.");
    const role = screen.getByRole("textbox", { name: /Role mention/ });
    expect(role.getAttribute("aria-invalid")).toBe("true");
    expect(role.getAttribute("aria-describedby")).toBe(issue.id);
    expect(issue.id).toBe("radarr-webhook-role-error");
  });
});

describe("existing Radarr webhook destination", () => {
  it("never tests the old saved payload after unsaved format changes", async () => {
    api.testDraft.mockResolvedValue({ verified: true });
    renderDialog({
      id: 8,
      name: "Movie night Discord",
      format: "discord",
      enabled: true,
      verified: true,
      reasons: ["preset_required"],
      revision: 2,
    });

    fireEvent.change(screen.getByRole("combobox", { name: "Format" }), {
      target: { value: "generic" },
    });

    const testButton = screen.getByRole("button", { name: "Send test" });
    expect((testButton as HTMLButtonElement).disabled).toBe(true);
    expect(screen.getByText(/Save these payload changes first/)).toBeTruthy();
    expect(api.testSaved).not.toHaveBeenCalled();

    fireEvent.change(screen.getByPlaceholderText("Enter a replacement URL"), {
      target: { value: "https://example.com/moviepickarr" },
    });
    fireEvent.click(testButton);

    await waitFor(() => expect(api.testDraft).toHaveBeenCalledOnce());
    expect(api.testDraft.mock.calls[0][0]).toMatchObject({
      format: "generic",
      url: "https://example.com/moviepickarr",
    });
    expect(api.testSaved).not.toHaveBeenCalled();
  });
});
