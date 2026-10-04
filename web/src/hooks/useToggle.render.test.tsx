/* Render test for useToggle: a fresh closure per render would defeat the memo
   of every child it is handed to. */

import { act, render, screen } from "@testing-library/react";
import { memo, useState } from "react";
import { describe, expect, it } from "vitest";

import { useToggle } from "@/hooks/hooks";

let childRenders = 0;

const Panel = memo(function Panel({ onToggle }: { onToggle: () => void }) {
  childRenders++;
  return (
    <button type="button" onClick={onToggle}>
      toggle
    </button>
  );
});

/** A parent that re-renders on its own, handing the toggle to a memoized child. */
function Harness() {
  const [on, toggle] = useToggle();
  const [tick, setTick] = useState(0);
  return (
    <>
      <span data-testid="state">{on ? "on" : "off"}</span>
      <button type="button" onClick={() => setTick((t) => t + 1)}>
        tick {tick}
      </button>
      <Panel onToggle={toggle} />
    </>
  );
}

describe("useToggle", () => {
  it("flips the value on every call", async () => {
    render(<Harness />);
    const state = screen.getByTestId("state");
    const toggle = screen.getByRole("button", { name: "toggle" });
    expect(state.textContent).toBe("off");

    await act(async () => toggle.click());
    expect(state.textContent).toBe("on");

    await act(async () => toggle.click());
    expect(state.textContent).toBe("off");
  });

  it("leaves a memoized child alone when the parent re-renders for other reasons", async () => {
    render(<Harness />);
    childRenders = 0;

    const tick = screen.getByRole("button", { name: /^tick/ });
    await act(async () => tick.click());
    await act(async () => tick.click());

    expect(childRenders).toBe(0);
  });
});
