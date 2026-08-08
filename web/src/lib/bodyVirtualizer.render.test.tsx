import { useVirtualizer } from "@tanstack/react-virtual";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";

import { documentScrollOwner } from "@/lib/scrollPolicy";

function BodyVirtualizer() {
  const virtualizer = useVirtualizer({
    count: 200,
    getScrollElement: documentScrollOwner,
    estimateSize: () => 100,
    observeElementRect: (_instance, callback) => {
      callback({ width: 1280, height: 600 });
      return () => {};
    },
    overscan: 0,
  });

  return (
    <output data-testid="visible-rows">
      {virtualizer.getVirtualItems().map((row) => row.index).join(",")}
    </output>
  );
}

afterEach(() => {
  document.body.scrollTop = 0;
});

describe("body-owned virtualization", () => {
  it("updates visible rows after the body scrolls past 5000px", async () => {
    render(<BodyVirtualizer />);
    await waitFor(() => expect(screen.getByTestId("visible-rows").textContent).toContain("0"));

    document.body.scrollTop = 5100;
    fireEvent.scroll(document.body);

    await waitFor(() => {
      const indices = (screen.getByTestId("visible-rows").textContent ?? "")
        .split(",")
        .filter(Boolean)
        .map(Number);
      expect(indices[0]).toBeGreaterThanOrEqual(50);
      expect(indices).not.toContain(0);
    });
  });
});
