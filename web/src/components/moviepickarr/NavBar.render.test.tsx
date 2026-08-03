/* ============================================================
   Render test for the navbar's role-gated Admin tab (#140).

   Which tabs an actor gets is a pure question and nav.test.ts owns it: the
   whole role matrix is asserted there against tabsForRole, and none of it is
   repeated here. What that pure test can't see is whether the answer survives
   the trip into the DOM. The tab list is rendered twice (top bar and the phone
   bottom bar) off a `me` that arrives from a query, so a gate that works in the
   model can still leak a link a member shouldn't have.

   That's all this file checks: an admin gets the Admin link, a member doesn't,
   in both bars. The sliding underline is layout, and jsdom has no layout
   engine, so it isn't asserted here.
   ============================================================ */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { AuthKeys } from "@/api/query_keys";

import { AudioProvider } from "@/components/AudioProvider";
import { NavBar } from "@/components/moviepickarr/NavBar";
import { ThemeProvider } from "@/components/ThemeProvider";

import type { MeResponse } from "@/types/Response";
import type { ReactNode, Ref } from "react";

// The tabs are router Links; outside a router there's no Link and no location.
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
  // Run the real selector over a stub location, so tabFromPath still decides
  // which tab is active rather than the mock hard-coding an answer.
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
  // Seed the actor rather than let the query fetch: the gate is the subject,
  // not the request that feeds it.
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
    // The tabs a member does get are still there, so this isn't an empty render.
    expect(screen.getAllByRole("link", { name: "Movies" }).length).toBeGreaterThan(0);
  });
});
