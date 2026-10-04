/* Render test for the role-gated Admin tab (#140) in both bars. The role matrix
   is nav.test.ts's; this checks the gate survives the trip into the DOM. */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { AuthKeys } from "@/api/query_keys";

import { AudioProvider } from "@/components/AudioProvider";
import { NavBar } from "@/components/moviepickarr/NavBar";
import { ThemeProvider } from "@/components/ThemeProvider";

import type { MeResponse } from "@/types/Response";
import type { ReactNode, Ref } from "react";

vi.mock("@tanstack/react-router", () => ({
  Link: ({
    to,
    children,
    ref,
    ...rest
  }: {
    to: string;
    children: ReactNode;
    ref?: Ref<HTMLAnchorElement>;
  }) => (
    <a href={to} ref={ref} {...rest}>
      {children}
    </a>
  ),
  // Run the real selector, so tabFromPath still picks the active tab.
  useRouterState: ({ select }: { select: (s: { location: { pathname: string } }) => unknown }) =>
    select({ location: { pathname: "/" } }),
  useNavigate: () => vi.fn(),
}));

vi.mock("@/api/APIClient", () => ({
  APIClient: { auth: { me: vi.fn(), logout: vi.fn() } },
}));

function actor(role: MeResponse["role"]): MeResponse {
  return {
    id: 1,
    displayName: "Cleo",
    username: "cleo",
    role,
    hasLocalLogin: true,
    hasLinkedIdentity: false,
  };
}

function renderNav(role: MeResponse["role"]) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Infinity } },
  });
  client.setQueryData(AuthKeys.me(), actor(role));

  render(
    <QueryClientProvider client={client}>
      <ThemeProvider defaultTheme="dark" storageKey="test-ui-theme">
        <AudioProvider>
          <NavBar />
        </AudioProvider>
      </ThemeProvider>
    </QueryClientProvider>,
  );
}

describe("the Admin tab", () => {
  it("reaches the DOM for an admin, in both the top and bottom bars", () => {
    renderNav("admin");

    const admin = screen.getAllByRole("link", { name: "Admin" });
    expect(admin).toHaveLength(2);
    expect(admin.every((link) => link.getAttribute("href") === "/admin")).toBe(true);
  });

  it("is absent for a member, so there's no link to a page they can't use", () => {
    renderNav("member");

    expect(screen.queryByRole("link", { name: "Admin" })).toBeNull();
    // Not an empty render.
    expect(screen.getAllByRole("link", { name: "Movies" }).length).toBeGreaterThan(0);
  });
});
