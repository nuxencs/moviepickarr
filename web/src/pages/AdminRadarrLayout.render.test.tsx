import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import type { ReactNode } from "react";

import { AdminRadarrLayout } from "@/pages/AdminRadarrLayout";

vi.mock("@tanstack/react-router", () => ({
  Link: (props: {
    children: ReactNode;
    className?: string;
    to: string;
    "aria-current"?: "page";
    "data-active"?: boolean;
  }) => (
    <a
      href={props.to}
      className={props.className}
      aria-current={props["aria-current"]}
      data-active={props["data-active"]}
    >
      {props.children}
    </a>
  ),
  Outlet: () => <div>Page content</div>,
  useRouterState: ({ select }: { select: (state: { location: { pathname: string } }) => unknown }) =>
    select({ location: { pathname: "/admin/integrations/radarr/setup" } }),
}));
vi.mock("@/hooks/useSlidingTabIndicator", () => ({
  useSlidingTabIndicator: () => ({
    position: { left: 36, width: 42 },
    setItemRef: vi.fn(),
  }),
}));

describe("Radarr section navigation", () => {
  it("marks exactly one current page", () => {
    render(<AdminRadarrLayout />);

    const setup = screen.getByRole("link", { name: "Setup" });
    expect(setup.getAttribute("aria-current")).toBe("page");
    expect(setup.getAttribute("data-active")).toBe("true");
    expect(screen.getAllByRole("link").filter((link) => link.getAttribute("aria-current") === "page")).toHaveLength(1);
    expect(screen.queryByRole("link", { name: "Radarr" })).toBeNull();
    expect(screen.getByRole("heading", { name: "Radarr" })).toBeTruthy();
    expect(document.querySelector(".radarr-workspace__header")?.classList.contains("mg-rise")).toBe(true);
    const ink = document.querySelector<HTMLElement>(".radarr-workspace__ink");
    expect(ink?.style.left).toBe("36px");
    expect(ink?.style.width).toBe("42px");
  });
});
