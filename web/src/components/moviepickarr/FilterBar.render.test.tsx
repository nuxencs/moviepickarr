import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { FilterMultiSelect } from "@/components/moviepickarr/FilterBar";

describe("active filter chips", () => {
  it("separates a long label from the fixed chevron and clear action", () => {
    const onChange = vi.fn();
    render(
      <FilterMultiSelect
        label="Added by"
        values={[7]}
        choices={[{ value: 7, label: "Christopher Nolan" }]}
        onChange={onChange}
      />,
    );

    const trigger = screen.getByRole("button", {
      name: "Added by · Christopher Nolan",
    });
    expect(trigger.querySelector(".filterchip__label")?.textContent).toBe(
      "Added by · Christopher Nolan",
    );

    const clear = screen.getByRole("button", {
      name: "Clear added by filter",
    });
    fireEvent.click(clear);

    expect(onChange).toHaveBeenCalledWith([]);
    expect(document.activeElement).toBe(trigger);
  });
});
