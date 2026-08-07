import { Link, Outlet, useRouterState } from "@tanstack/react-router";

import "@/components/moviepickarr/admin/radarr.css";

export function AdminRadarrLayout() {
  const pathname = useRouterState({ select: (state) => state.location.pathname });
  const setup = pathname.startsWith("/admin/integrations/radarr/setup");
  const webhooks = pathname.startsWith("/admin/integrations/radarr/webhooks");
  const acquisitions = !setup && !webhooks;

  return (
    <div className="admin-section radarr-workspace mg-rise">
      <nav className="radarr-workspace__nav" aria-label="Radarr sections">
        <Link
          to="/admin/integrations/radarr"
          activeOptions={{ exact: true }}
          data-active={acquisitions}
          aria-current={acquisitions ? "page" : undefined}
        >
          Acquisitions
        </Link>
        <Link
          to="/admin/integrations/radarr/setup"
          activeOptions={{ exact: true }}
          data-active={setup}
          aria-current={setup ? "page" : undefined}
        >
          Setup
        </Link>
        <Link
          to="/admin/integrations/radarr/webhooks"
          activeOptions={{ exact: true }}
          data-active={webhooks}
          aria-current={webhooks ? "page" : undefined}
        >
          Webhooks
        </Link>
      </nav>
      <Outlet />
    </div>
  );
}
