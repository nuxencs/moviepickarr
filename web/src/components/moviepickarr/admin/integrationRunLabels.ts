import {
  INTEGRATION_RUN_RESULT_STATUSES,
  type IntegrationRunOperation,
  type IntegrationRunResultStatus,
  type IntegrationRunStatus,
  type IntegrationRunTrigger,
  type KnownIntegrationRunTrigger,
  type TMDBRunOperation,
} from "@/api/integrations";

export const INTEGRATION_RUN_STATUS_LABELS: Record<IntegrationRunStatus, string> = {
  running: "Running",
  completed: "Completed",
  completed_with_errors: "Completed with errors",
  failed: "Failed",
  cancelled: "Cancelled",
  interrupted: "Interrupted",
};

export const TMDB_RUN_OPERATION_LABELS: Record<TMDBRunOperation, string> = {
  refresh_stale: "Refresh stale",
  re_enrich_all: "Re-enrich all",
  enrich_movie: "Enrich movie",
};

const TRIGGER_LABELS: Record<KnownIntegrationRunTrigger, string> = {
  scheduled: "Scheduled",
  manual: "Manual",
  movie_added: "Movie added",
  movie_updated: "Movie updated",
  configuration: "Configuration",
  startup: "Startup",
};

export const INTEGRATION_RUN_RESULT_OPTIONS = INTEGRATION_RUN_RESULT_STATUSES.map(
  (status) => [status, INTEGRATION_RUN_STATUS_LABELS[status]] as const,
) satisfies ReadonlyArray<readonly [IntegrationRunResultStatus, string]>;

function identifierLabel(id: string) {
  const words = id.split(/[-_]+/).filter(Boolean);
  if (words.length === 0) return "Unknown";
  return words
    .map((word) => `${word.charAt(0).toUpperCase()}${word.slice(1).toLowerCase()}`)
    .join(" ");
}

export function integrationRunOperationLabel(operation: IntegrationRunOperation) {
  return TMDB_RUN_OPERATION_LABELS[operation as TMDBRunOperation] ?? identifierLabel(operation);
}

export function integrationRunTriggerLabel(trigger: IntegrationRunTrigger) {
  return TRIGGER_LABELS[trigger as KnownIntegrationRunTrigger] ?? identifierLabel(trigger);
}
