import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";

import {
  IntegrationKeys,
  IntegrationProblem,
  type IntegrationState,
  listIntegrations,
} from "@/api/integrations";

import { plural } from "@/components/moviepickarr/lib";

import "@/components/moviepickarr/admin/integrations.css";

const STATE_LABELS: Record<IntegrationState, string> = {
  disabled: "Disabled",
  connected: "Connected",
  could_not_verify: "Could not verify",
  error: "Error",
  credential_unavailable: "Credential unavailable",
};

const ACTIVITY_FORMATTER = new Intl.DateTimeFormat("en-US", {
  day: "numeric",
  hour: "numeric",
  minute: "2-digit",
  month: "short",
  timeZone: "UTC",
  timeZoneName: "short",
  year: "numeric",
});

function ActivityTime({ value }: { value?: string }) {
  if (!value) return <>No activity recorded</>;
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return <>Activity recorded</>;
  return <time dateTime={value}>{ACTIVITY_FORMATTER.format(date)}</time>;
}

const INTEGRATION_UI = {
  tmdb: {
    description: "Movie metadata and artwork",
    to: "/admin/integrations/tmdb",
  },
  radarr: {
    description: "Current-draw acquisition",
    to: "/admin/integrations/radarr",
  },
} as const;

function integrationUI(id: string) {
  return INTEGRATION_UI[id as keyof typeof INTEGRATION_UI] ?? {
    description: "Connected service",
    to: "/admin/integrations" as const,
  };
}

export function AdminIntegrationsPage() {
  const integrations = useQuery({
    queryKey: IntegrationKeys.list(),
    queryFn: ({ signal }) => listIntegrations(signal),
    retry: false,
  });

  return (
    <section className="admin-section integration-index mg-rise" aria-labelledby="integration-index-title">
      <div className="sec-head">
        <div className="sec-title">
          <h2 id="integration-index-title">Integrations</h2>
          {integrations.data ? (
            <span className="sec-count">{plural(integrations.data.length, "integration")}</span>
          ) : null}
        </div>
      </div>

      {integrations.isPending ? (
        <div className="adm-state" role="status">
          Loading integrations…
        </div>
      ) : integrations.isError ? (
        <div className="adm-state" role="alert">
          {integrations.error instanceof IntegrationProblem && integrations.error.status === 403
            ? "Admin access is required to view integrations."
            : "Integrations could not be loaded."}
        </div>
      ) : integrations.data.length === 0 ? (
        <div className="adm-state">No integrations are available.</div>
      ) : (
        <ul className="integration-index__list" aria-label="Integrations">
          {integrations.data.map((integration) => (
            <li key={integration.id}>
              <Link to={integrationUI(integration.id).to} className="integration-index__row">
                <span className="integration-index__identity">
                  <strong>{integration.name}</strong>
                  <span>{integrationUI(integration.id).description}</span>
                </span>
                <span className="integration-index__state" data-state={integration.state}>
                  <strong>{STATE_LABELS[integration.state]}</strong>
                  <span>{integration.reason || "No connection issue reported"}</span>
                </span>
                <span className="integration-index__activity">
                  <span>Latest activity</span>
                  <ActivityTime value={integration.latestActivity} />
                </span>
              </Link>
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}
