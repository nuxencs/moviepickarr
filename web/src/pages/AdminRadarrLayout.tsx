import { Link, Outlet, useRouterState } from "@tanstack/react-router";

import "@/components/moviepickarr/admin/radarr.css";
import { useSlidingTabIndicator } from "@/hooks/useSlidingTabIndicator";

type RadarrSection = "acquisitions" | "setup" | "webhooks";

export function AdminRadarrLayout() {
  const pathname = useRouterState({ select: (state) => state.location.pathname });
  const setup = pathname.startsWith("/admin/integrations/radarr/setup");
  const webhooks = pathname.startsWith("/admin/integrations/radarr/webhooks");
  const acquisitions = !setup && !webhooks;
  const active: RadarrSection = setup ? "setup" : webhooks ? "webhooks" : "acquisitions";
  const { position: ink, setItemRef } = useSlidingTabIndicator(active, 3);

  return (
    <div className="admin-section radarr-workspace">
      <header className="radarr-workspace__header mg-rise">
        <h2 className="vis-hidden">Radarr</h2>
        <nav className="radarr-workspace__nav" aria-label="Radarr sections">
          <Link
            to="/admin/integrations/radarr"
            ref={(element) => setItemRef("acquisitions", element)}
            activeOptions={{ exact: true }}
            data-active={acquisitions}
            aria-current={acquisitions ? "page" : undefined}
          >
            Acquisitions
          </Link>
          <Link
            to="/admin/integrations/radarr/setup"
            ref={(element) => setItemRef("setup", element)}
            activeOptions={{ exact: true }}
            data-active={setup}
            aria-current={setup ? "page" : undefined}
          >
            Setup
          </Link>
          <Link
            to="/admin/integrations/radarr/webhooks"
            ref={(element) => setItemRef("webhooks", element)}
            activeOptions={{ exact: true }}
            data-active={webhooks}
            aria-current={webhooks ? "page" : undefined}
          >
            Webhooks
          </Link>
          {ink ? (
            <span
              className="radarr-workspace__ink"
              style={{ left: ink.left, width: ink.width }}
              aria-hidden="true"
            />
          ) : null}
        </nav>
      </header>
      <Outlet />
    </div>
  );
}
