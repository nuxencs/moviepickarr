import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { RadarrDisclosure } from "@/components/moviepickarr/admin/RadarrDisclosure";

describe("RadarrDisclosure", () => {
  it("uses one accessible animated state while preserving concealed content", () => {
    render(
      <RadarrDisclosure title="History" meta={3}>
        <label>
          Search history
          <input defaultValue="Arrival" />
        </label>
      </RadarrDisclosure>,
    );

    const trigger = screen.getByRole("button", { name: "History" });
    const contentID = trigger.getAttribute("aria-controls");
    const content = contentID ? document.getElementById(contentID) : null;
    const viewport = content?.parentElement?.parentElement;

    expect(trigger.getAttribute("aria-expanded")).toBe("false");
    expect(trigger.getAttribute("aria-describedby")).toBe(
      trigger.querySelector(".radarr-disclosure__meta")?.id,
    );
    expect(viewport?.hasAttribute("inert")).toBe(true);
    expect(screen.queryByRole("region", { name: "History" })).toBeNull();

    fireEvent.click(trigger);

    expect(trigger.getAttribute("aria-expanded")).toBe("true");
    expect(trigger.closest(".radarr-disclosure")?.getAttribute("data-open")).toBe("true");
    expect(viewport?.hasAttribute("inert")).toBe(false);
    expect(screen.getByRole("region", { name: "History" })).toBeTruthy();

    const search = screen.getByRole("textbox", { name: "Search history" });
    fireEvent.change(search, { target: { value: "Heat" } });
    fireEvent.click(trigger);
    fireEvent.click(trigger);

    expect((screen.getByRole("textbox", { name: "Search history" }) as HTMLInputElement).value).toBe("Heat");
    expect(trigger.querySelector("svg")?.getAttribute("aria-hidden")).toBe("true");
  });
});
