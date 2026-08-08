export type IntegrationState =
  | "disabled"
  | "connected"
  | "could_not_verify"
  | "error"
  | "credential_unavailable";

export type IntegrationSource = "default" | "admin" | "environment";

export interface IntegrationSummary {
  id: string;
  name: string;
  state: IntegrationState;
  reason?: string;
  latestActivity?: string;
  operations: Array<{ id: IntegrationRunOperation; name: string }>;
}

export interface IntegrationSetting<T> {
  value: T;
  source: IntegrationSource;
  default: T;
  hasAdminFallback: boolean;
  environment: string;
}

export interface SecretIntegrationSetting {
  configured: boolean;
  source: IntegrationSource;
  hasAdminFallback: boolean;
  environment: string;
}

export interface TMDBSettings {
  enabled: IntegrationSetting<boolean>;
  apiKey: SecretIntegrationSetting;
  castLimit: IntegrationSetting<number>;
  refreshIntervalMs: IntegrationSetting<number>;
  ttlMs: IntegrationSetting<number>;
  minIntervalMs: IntegrationSetting<number>;
  maxRetries: IntegrationSetting<number>;
  backoffMs: IntegrationSetting<number>;
  batchLimit: IntegrationSetting<number>;
}

export interface TMDBIntegration {
  revision: number;
  state: IntegrationState;
  reason?: string;
  warnings?: IntegrationProblemItem[];
  settings: TMDBSettings;
  latestRun?: IntegrationRun;
  lastCheckedAt?: string;
  lastConnectionTestedAt?: string;
  nextCheckAt?: string;
  lastSuccessfulRunAt?: string;
}

export const TMDB_RUN_OPERATIONS = ["refresh_stale", "re_enrich_all", "enrich_movie"] as const;
export type TMDBRunOperation = (typeof TMDB_RUN_OPERATIONS)[number];
export type TMDBLibraryRunOperation = Extract<TMDBRunOperation, "refresh_stale" | "re_enrich_all">;
export type IntegrationRunOperation = string;
export type IntegrationRunTrigger = string;
export const KNOWN_INTEGRATION_RUN_TRIGGERS = [
  "scheduled",
  "manual",
  "movie_added",
  "movie_updated",
  "configuration",
  "startup",
] as const;
export type KnownIntegrationRunTrigger = (typeof KNOWN_INTEGRATION_RUN_TRIGGERS)[number];
export const INTEGRATION_RUN_RESULT_STATUSES = [
  "completed",
  "completed_with_errors",
  "failed",
  "cancelled",
  "interrupted",
] as const;
export type IntegrationRunResultStatus = (typeof INTEGRATION_RUN_RESULT_STATUSES)[number];
export type IntegrationRunStatus = "running" | IntegrationRunResultStatus;

export interface IntegrationRunProgress {
  total: number;
  processed: number;
  succeeded: number;
  failed: number;
  skipped: number;
  remaining: number;
}

export interface IntegrationRun {
  id: number;
  integration: string;
  operation: IntegrationRunOperation;
  trigger: IntegrationRunTrigger;
  initiatedBy?: number | null;
  configRevision: number;
  status: IntegrationRunStatus;
  startedAt: string;
  finishedAt?: string;
  progress: IntegrationRunProgress;
  errorSummary?: string;
  failedSubjects: Array<{ subject: string; error: string }>;
}

export type IntegrationRunResult = Omit<IntegrationRun, "finishedAt" | "status"> & {
  finishedAt: string;
  status: IntegrationRunResultStatus;
};

export interface IntegrationRunHistoryQuery {
  integration?: string;
  operation?: IntegrationRunOperation;
  status?: IntegrationRunResultStatus;
  trigger?: IntegrationRunTrigger;
  cursor?: string;
  limit: number;
}

export interface IntegrationRunHistoryPage {
  runs: IntegrationRunResult[];
  nextCursor?: string;
}

export interface TMDBNoWorkResult {
  noWork: true;
}

export interface TMDBSettingsDraft {
  enabled?: boolean;
  castLimit?: number;
  refreshIntervalMs?: number;
  ttlMs?: number;
  minIntervalMs?: number;
  maxRetries?: number;
  backoffMs?: number;
  batchLimit?: number;
}

export interface TMDBDraftRequest {
  revision: number;
  settings: TMDBSettingsDraft;
  removeFallbacks: string[];
  apiKey: string;
  clearApiKey: boolean;
  confirmWarnings: boolean;
}

export interface TMDBConnectionResult {
  state: IntegrationState;
  reason?: string;
  checkedAt: string;
}

export interface IntegrationProblemItem {
  field: string;
  message: string;
}

interface ProblemBody {
  title?: unknown;
  detail?: unknown;
  issues?: unknown;
  warnings?: unknown;
}

export class IntegrationProblem extends Error {
  readonly status: number;
  readonly title: string;
  readonly issues: IntegrationProblemItem[];
  readonly warnings: IntegrationProblemItem[];

  constructor(
    status: number,
    title: string,
    detail: string,
    issues: IntegrationProblemItem[] = [],
    warnings: IntegrationProblemItem[] = [],
  ) {
    super(detail);
    this.name = "IntegrationProblem";
    this.status = status;
    this.title = title;
    this.issues = issues;
    this.warnings = warnings;
  }
}

function problemItems(value: unknown): IntegrationProblemItem[] {
  if (!Array.isArray(value)) return [];
  return value.flatMap((item) => {
    if (
      typeof item === "object" &&
      item !== null &&
      typeof (item as { field?: unknown }).field === "string" &&
      typeof (item as { message?: unknown }).message === "string"
    ) {
      return [item as IntegrationProblemItem];
    }
    return [];
  });
}

export async function integrationRequest<T>(
  path: string,
  init: RequestInit,
): Promise<T> {
  const response = await fetch(path, {
    ...init,
    credentials: "include",
    headers: {
      Accept: "application/json",
      ...(init.body ? { "Content-Type": "application/json" } : {}),
      ...init.headers,
    },
  });
  const contentType = response.headers.get("Content-Type") ?? "";
  const isJSON = contentType.includes("application/json") || contentType.includes("+json");

  if (response.ok) {
    return (isJSON ? await response.json() : response) as T;
  }

  let body: ProblemBody = {};
  if (isJSON) {
    try {
      body = (await response.json()) as ProblemBody;
    } catch {
      body = {};
    }
  }
  const title = typeof body.title === "string" ? body.title : "request_failed";
  const detail =
    typeof body.detail === "string" && body.detail.length > 0
      ? body.detail
      : `Request failed with status ${response.status}`;
  throw new IntegrationProblem(
    response.status,
    title,
    detail,
    problemItems(body.issues),
    problemItems(body.warnings),
  );
}

export const IntegrationKeys = {
  all: ["integrations"] as const,
  list: () => [...IntegrationKeys.all, "list"] as const,
  tmdb: () => [...IntegrationKeys.all, "tmdb"] as const,
  runHistory: () => [...IntegrationKeys.all, "runs"] as const,
  runs: (query: IntegrationRunHistoryQuery) => [...IntegrationKeys.runHistory(), query] as const,
};

export function listIntegrations(signal?: AbortSignal) {
  return integrationRequest<IntegrationSummary[]>("/api/v1/integrations", { method: "GET", signal });
}

export function getTMDBIntegration(signal?: AbortSignal) {
  return integrationRequest<TMDBIntegration>("/api/v1/integrations/tmdb", { method: "GET", signal });
}

export function saveTMDBIntegration(draft: TMDBDraftRequest) {
  return integrationRequest<TMDBIntegration>("/api/v1/integrations/tmdb", {
    method: "PUT",
    body: JSON.stringify(draft),
  });
}

export function testTMDBConnection(draft: TMDBDraftRequest) {
  return integrationRequest<TMDBConnectionResult>("/api/v1/integrations/tmdb/test", {
    method: "POST",
    body: JSON.stringify(draft),
  });
}

export function startTMDBRun(
  operation: TMDBLibraryRunOperation,
  confirm: boolean,
) {
  return integrationRequest<IntegrationRun | TMDBNoWorkResult>("/api/v1/integrations/tmdb/runs", {
    method: "POST",
    body: JSON.stringify({ operation, confirm }),
  });
}

export function cancelIntegrationRun(runID: number) {
  return integrationRequest<void>(`/api/v1/integration-runs/${runID}`, { method: "DELETE" });
}

export function listIntegrationRuns(
  query: IntegrationRunHistoryQuery,
  signal?: AbortSignal,
) {
  const search = new URLSearchParams();
  if (query.integration) search.set("integration", query.integration);
  if (query.operation) search.set("operation", query.operation);
  if (query.status) search.set("status", query.status);
  if (query.trigger) search.set("trigger", query.trigger);
  if (query.cursor) search.set("cursor", query.cursor);
  search.set("limit", String(query.limit));
  return integrationRequest<IntegrationRunHistoryPage>(`/api/v1/integration-runs?${search}`, {
    method: "GET",
    signal,
  });
}
