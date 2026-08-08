import { Link, Outlet, useRouter, useRouterState } from "@tanstack/react-router";
import { HistoryIcon, PlugIcon, UsersIcon } from "lucide-react";
import { useLayoutEffect, useRef } from "react";

import { RadarrAttentionBadge } from "@/components/moviepickarr/admin/RadarrAttentionBadge";
import { Shell } from "@/components/moviepickarr/AppShell";

import "@/components/moviepickarr/admin/admin-layout.css";

type AdminSection = "roster" | "integrations" | "runs";

const ADMIN_BODY_LABELS: Record<AdminSection, string> = {
  roster: "Member roster",
  integrations: "Selected integration configuration",
  runs: "Integration run history",
};

function adminBodyLabel(pathname: string, active?: AdminSection) {
  if (pathname === "/admin/integrations") return "Integration catalog";
  if (pathname.includes("/integrations/radarr/setup")) return "Radarr setup";
  if (pathname.includes("/integrations/radarr/webhooks")) return "Radarr webhooks";
  if (pathname.includes("/integrations/radarr/acquisitions/")) return "Radarr acquisition details";
  if (pathname.startsWith("/admin/integrations/radarr")) return "Radarr acquisitions";
  return active ? ADMIN_BODY_LABELS[active] : "Admin content";
}

function adminSectionFromPath(pathname: string): AdminSection | undefined {
  if (pathname.startsWith("/admin/integrations")) return "integrations";
  if (pathname === "/admin/runs") return "runs";
  if (pathname === "/admin/roster") return "roster";
  return undefined;
}

/** Shared wayfinding for the Admin pages. Child routes stay separate chunks. */
export function AdminLayout() {
  const router = useRouter();
  const pathname = useRouterState({ select: (state) => state.location.pathname });
  const renderedPathname = useRouterState({
    select: (state) => state.matches[state.matches.length - 1]?.pathname,
  });
  const active = adminSectionFromPath(pathname);
  const bodyLabel = adminBodyLabel(pathname, active);
  const navRef = useRef<HTMLElement>(null);
  const bodyRef = useRef<HTMLDivElement>(null);
  const integrationLabel = (
    <>
      <PlugIcon aria-hidden="true" />
      <span>Integrations</span>
    </>
  );

  useLayoutEffect(() => {
    if (bodyRef.current) bodyRef.current.scrollTop = 0;
  }, [renderedPathname]);

  useLayoutEffect(() => {
    // The Admin index scrolls sideways on mobile. Keep the committed leaf in
    // view on direct loads and route changes without moving the page vertically.
    const selected = navRef.current?.querySelector<HTMLElement>('[aria-current="page"]');
    selected?.scrollIntoView?.({ block: "nearest", inline: "nearest" });
  }, [pathname]);

  const openIntegrations = () => {
    void router.navigate({ to: "/admin/integrations" });
  };

  return (
    <Shell className="shell--admin">
      <div className="admin-layout" data-section={active}>
        <span className="vis-hidden" role="status" aria-live="polite" aria-atomic="true">
          {bodyLabel}
        </span>
        <nav
          ref={navRef}
          className="admin-layout__nav mg-rise"
          aria-label="Admin sections"
          data-page-scroll-owner
        >
          <span className="admin-layout__indicator" aria-hidden="true" />
          <Link
            to="/admin/roster"
            activeOptions={{ exact: true }}
            className="admin-layout__link"
            data-active={active === "roster"}
            aria-current={active === "roster" ? "page" : undefined}
          >
            <UsersIcon aria-hidden="true" />
            <span>Roster</span>
          </Link>

          <div
            className="admin-layout__branch"
            data-active={active === "integrations"}
            role="group"
            aria-label="Integrations"
          >
            <button
              type="button"
              className="admin-layout__link"
              data-active={active === "integrations"}
              aria-expanded={active === "integrations"}
              aria-current={pathname === "/admin/integrations" ? "page" : undefined}
              aria-controls="admin-layout-integrations"
              onClick={openIntegrations}
            >
              {integrationLabel}
            </button>

            <div
              id="admin-layout-integrations"
              className="admin-layout__children"
              data-open={active === "integrations"}
              inert={active !== "integrations"}
              aria-hidden={active !== "integrations"}
            >
              <div className="admin-layout__children-inner">
                <Link
                  to="/admin/integrations/tmdb"
                  activeOptions={{ exact: true }}
                  className="admin-layout__integration"
                >
                  <span>TMDB</span>
                </Link>
                <Link
                  to="/admin/integrations/radarr"
                  className="admin-layout__integration"
                >
                  <span>Radarr</span>
                  <RadarrAttentionBadge />
                </Link>
              </div>
            </div>
          </div>

          <Link
            to="/admin/runs"
            activeOptions={{ exact: true }}
            className="admin-layout__link"
            data-active={active === "runs"}
            aria-current={active === "runs" ? "page" : undefined}
          >
            <HistoryIcon aria-hidden="true" />
            <span>Runs</span>
          </Link>
        </nav>

        <div
          ref={bodyRef}
          className="admin-layout__body"
          data-page-scroll-owner
          role="region"
          aria-label={bodyLabel}
          tabIndex={0}
        >
          <Outlet />
        </div>
      </div>
    </Shell>
  );
}
