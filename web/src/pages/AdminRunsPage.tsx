import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { useLocation, useNavigate, useSearch } from "@tanstack/react-router";
import { ChevronRightIcon, XIcon } from "lucide-react";
import { useEffect, useMemo, useState } from "react";

import {
  IntegrationKeys,
  IntegrationProblem,
  listIntegrationRuns,
  listIntegrations,
  type IntegrationRun,
  type IntegrationRunHistoryQuery,
  type IntegrationRunOperation,
  type IntegrationRunResult,
  type IntegrationRunResultStatus,
} from "@/api/integrations";

import {
  INTEGRATION_RUN_RESULT_OPTIONS,
  INTEGRATION_RUN_STATUS_LABELS,
  integrationRunOperationLabel,
  integrationRunTriggerLabel,
  TMDB_RUN_OPERATION_LABELS,
} from "@/components/moviepickarr/admin/integrationRunLabels";
import { plural } from "@/components/moviepickarr/lib";
import { Modal } from "@/components/moviepickarr/Modal";

import type { AdminRunsSearch } from "@/pages/adminRunsSearch";

import "@/pages/admin-runs.css";

declare module "@tanstack/react-router" {
  interface HistoryState {
    adminRunPreviousCursors?: Array<string | null>;
  }
}

const EMPTY_CURSOR_HISTORY: Array<string | null> = [];

const RUN_TIMESTAMP_FORMATTER = new Intl.DateTimeFormat("en-US", {
  day: "numeric",
  hour: "numeric",
  minute: "2-digit",
  month: "short",
  timeZone: "UTC",
  timeZoneName: "short",
  year: "numeric",
});

function isRunResult(run: unknown): run is IntegrationRunResult {
  if (!run || typeof run !== "object") return false;
  const candidate = run as Partial<IntegrationRun>;
  return (
    typeof candidate.finishedAt === "string" &&
    INTEGRATION_RUN_RESULT_OPTIONS.some(([status]) => status === candidate.status)
  );
}

function timestampLabel(value: string) {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "Recorded";
  return RUN_TIMESTAMP_FORMATTER.format(date);
}

function durationLabel(startedAt: string, finishedAt: string) {
  const durationMs = new Date(finishedAt).getTime() - new Date(startedAt).getTime();
  if (!Number.isFinite(durationMs) || durationMs < 0) return "Unavailable";
  const totalSeconds = Math.floor(durationMs / 1_000);
  if (totalSeconds < 1) return "Less than a second";
  const hours = Math.floor(totalSeconds / 3_600);
  const minutes = Math.floor((totalSeconds % 3_600) / 60);
  const seconds = totalSeconds % 60;
  const parts = [
    hours > 0 ? `${hours} hr` : "",
    minutes > 0 ? `${minutes} min` : "",
    seconds > 0 && hours === 0 ? `${seconds} sec` : "",
  ].filter(Boolean);
  return parts.join(" ");
}

function resultCountLabel(run: IntegrationRunResult) {
  const counts = [
    run.progress.succeeded > 0 ? `${run.progress.succeeded} succeeded` : "",
    run.progress.failed > 0 ? `${run.progress.failed} failed` : "",
    run.progress.skipped > 0 ? `${run.progress.skipped} skipped` : "",
  ].filter(Boolean);
  return counts.length > 0 ? counts.join(" · ") : "No subjects processed";
}

function fallbackIntegrationName(id: string) {
  return id.replace(/[-_]+/g, " ").toUpperCase();
}

function hasSameRunFilters(previousQueryKey: unknown, request: IntegrationRunHistoryQuery) {
  if (!previousQueryKey || typeof previousQueryKey !== "object") return false;
  const previous = previousQueryKey as Partial<IntegrationRunHistoryQuery>;
  return (
    previous.integration === request.integration &&
    previous.operation === request.operation &&
    previous.status === request.status &&
    previous.trigger === request.trigger &&
    previous.limit === request.limit
  );
}

function RunResultRow({
  integrationName,
  operationName,
  onOpen,
  run,
}: {
  integrationName: string;
  operationName: string;
  onOpen: () => void;
  run: IntegrationRunResult;
}) {
  return (
    <li className="run-register__item">
      <button
        type="button"
        className="run-result"
        aria-haspopup="dialog"
        onClick={onOpen}
      >
        <time className="run-result__when" dateTime={run.finishedAt}>
          {timestampLabel(run.finishedAt)}
        </time>
        <span className="run-result__identity">
          <strong>{integrationName}</strong>
          <span>{operationName}</span>
        </span>
        <span className="run-result__outcome">
          <span className="run-result__status" data-status={run.status}>
            {INTEGRATION_RUN_STATUS_LABELS[run.status]}
          </span>
          <span className="run-result__counts">{resultCountLabel(run)}</span>
        </span>
        <ChevronRightIcon className="run-result__chevron" aria-hidden="true" />
        <span className="vis-hidden">View run details</span>
      </button>
    </li>
  );
}

function RunDetailsModal({
  integrationName,
  operationName,
  onClose,
  run,
}: {
  integrationName: string;
  operationName: string;
  onClose: () => void;
  run: IntegrationRunResult;
}) {
  const failures = run.failedSubjects.slice(0, 25);
  const title = `${integrationName} · ${operationName}`;
  const counts = [
    ["Total", run.progress.total],
    ["Processed", run.progress.processed],
    ["Succeeded", run.progress.succeeded],
    ["Failed", run.progress.failed],
    ["Skipped", run.progress.skipped],
    ["Remaining", run.progress.remaining],
  ] as const;

  return (
    <Modal label={title} className="modal--run" capped onClose={onClose}>
      {(close) => (
        <>
          <header className="run-detail__head">
            <div>
              <h3>{title}</h3>
              <span className="run-result__status" data-status={run.status}>
                {INTEGRATION_RUN_STATUS_LABELS[run.status]}
              </span>
            </div>
            <button type="button" className="iconbtn" aria-label="Close" onClick={close}>
              <XIcon aria-hidden="true" />
            </button>
          </header>

          <div className="modal__scroll run-detail__scroll">
            <section className="run-detail__section" aria-labelledby="run-detail-timing">
              <h4 id="run-detail-timing">Timing</h4>
              <dl className="run-detail__facts">
                <div>
                  <dt>Started</dt>
                  <dd><time dateTime={run.startedAt}>{timestampLabel(run.startedAt)}</time></dd>
                </div>
                <div>
                  <dt>Finished</dt>
                  <dd><time dateTime={run.finishedAt}>{timestampLabel(run.finishedAt)}</time></dd>
                </div>
                <div>
                  <dt>Duration</dt>
                  <dd>{durationLabel(run.startedAt, run.finishedAt)}</dd>
                </div>
                <div>
                  <dt>Trigger</dt>
                  <dd>{integrationRunTriggerLabel(run.trigger)}</dd>
                </div>
              </dl>
            </section>

            <section className="run-detail__section" aria-labelledby="run-detail-results">
              <h4 id="run-detail-results">Results</h4>
              <dl className="run-detail__counts">
                {counts.map(([label, value]) => (
                  <div key={label}>
                    <dt>{label}</dt>
                    <dd>{value}</dd>
                  </div>
                ))}
              </dl>
            </section>

            {run.errorSummary || failures.length > 0 ? (
              <section className="run-detail__section" aria-labelledby="run-detail-failures">
                <h4 id="run-detail-failures">Failures</h4>
                {run.errorSummary ? <p className="run-detail__error-summary">{run.errorSummary}</p> : null}
                {failures.length > 0 ? (
                  <ul className="run-detail__failures">
                    {failures.map((failure, index) => (
                      <li key={`${failure.subject}-${index}`}>
                        <strong>{failure.subject}</strong>
                        <span>{failure.error}</span>
                      </li>
                    ))}
                  </ul>
                ) : null}
                {run.failedSubjects.length > failures.length ? (
                  <p className="run-detail__failure-limit">
                    Showing the first {failures.length} of {plural(run.failedSubjects.length, "failure")}.
                  </p>
                ) : null}
              </section>
            ) : null}
          </div>
        </>
      )}
    </Modal>
  );
}

export function AdminRunsPage() {
  const search = useSearch({ from: "/_app/admin/runs" });
  const navigate = useNavigate({ from: "/admin/runs" });
  const previousCursors = useLocation({
    select: (location) => location.state.adminRunPreviousCursors ?? EMPTY_CURSOR_HISTORY,
  });
  const [selectedRun, setSelectedRun] = useState<IntegrationRunResult | null>(null);
  const request = { ...search, limit: 50 };
  const filtered = Boolean(search.integration || search.operation || search.status);
  const history = useQuery({
    queryKey: IntegrationKeys.runs(request),
    queryFn: ({ signal }) => listIntegrationRuns(request, signal),
    placeholderData: (previousData, previousQuery) => {
      const previousKey = previousQuery?.queryKey[previousQuery.queryKey.length - 1];
      return hasSameRunFilters(previousKey, request) ? keepPreviousData(previousData) : undefined;
    },
    refetchOnMount: "always",
    retry: false,
    staleTime: 0,
  });
  const catalog = useQuery({
    queryKey: IntegrationKeys.list(),
    queryFn: ({ signal }) => listIntegrations(signal),
    retry: false,
  });
  const integrationNames = useMemo(
    () => new Map(catalog.data?.map((integration) => [integration.id, integration.name]) ?? []),
    [catalog.data],
  );
  const runs = history.data?.runs.filter(isRunResult) ?? [];
  const integrationOptions = useMemo(() => {
    const options = catalog.data ? [...catalog.data] : [];
    for (const run of history.data?.runs ?? []) {
      if (!isRunResult(run) || options.some((integration) => integration.id === run.integration)) {
        continue;
      }
      options.push({
        id: run.integration,
        name: fallbackIntegrationName(run.integration),
        state: "disabled",
        operations: [],
      });
    }
    if (search.integration && !options.some((integration) => integration.id === search.integration)) {
      options.push({
        id: search.integration,
        name: fallbackIntegrationName(search.integration),
        state: "disabled",
        operations: [],
      });
    }
    return options;
  }, [catalog.data, history.data?.runs, search.integration]);
  const operationOptions = useMemo(() => {
    const options = new Map<IntegrationRunOperation, string>();
    const catalogIntegrations = search.integration
      ? catalog.data?.filter((integration) => integration.id === search.integration)
      : catalog.data;
    if (catalogIntegrations) {
      for (const integration of catalogIntegrations) {
        for (const operation of integration.operations) options.set(operation.id, operation.name);
      }
    } else if (!search.integration || search.integration === "tmdb") {
      for (const [operation, label] of Object.entries(TMDB_RUN_OPERATION_LABELS)) {
        options.set(operation, label);
      }
    }
    for (const run of history.data?.runs ?? []) {
      if (isRunResult(run) && !options.has(run.operation)) {
        options.set(run.operation, integrationRunOperationLabel(run.operation));
      }
    }
    if (search.operation && !options.has(search.operation)) {
      options.set(search.operation, integrationRunOperationLabel(search.operation));
    }
    return [...options];
  }, [catalog.data, history.data?.runs, search.integration, search.operation]);
  const integrationNameFor = (id: string) => integrationNames.get(id) ?? fallbackIntegrationName(id);
  const operationNameFor = (id: IntegrationRunOperation) =>
    operationOptions.find(([operation]) => operation === id)?.[1] ?? integrationRunOperationLabel(id);

  useEffect(() => {
    setSelectedRun(null);
  }, [search.cursor, search.integration, search.operation, search.status]);

  const updateFilters = (updates: Partial<AdminRunsSearch>) => {
    setSelectedRun(null);
    void navigate({
      search: (previous) => ({ ...previous, ...updates, cursor: undefined }),
      state: (previous) => ({ ...previous, adminRunPreviousCursors: undefined }),
    });
  };
  const nextPage = () => {
    if (!history.data?.nextCursor || history.isPlaceholderData) return;
    setSelectedRun(null);
    const nextCursorHistory = [...previousCursors, search.cursor ?? null];
    void navigate({
      search: (previous) => ({ ...previous, cursor: history.data.nextCursor }),
      state: (previous) => ({
        ...previous,
        adminRunPreviousCursors: nextCursorHistory,
      }),
    });
  };
  const previousPage = () => {
    if ((previousCursors.length === 0 && !search.cursor) || history.isPlaceholderData) return;
    const cursor = previousCursors[previousCursors.length - 1] ?? null;
    setSelectedRun(null);
    void navigate({
      search: (previous) => ({ ...previous, cursor: cursor ?? undefined }),
      state: (previous) => ({
        ...previous,
        adminRunPreviousCursors: previousCursors.slice(0, -1),
      }),
    });
  };

  return (
    <section className="admin-section admin-runs mg-rise" aria-labelledby="admin-runs-title">
      <div className="sec-head">
        <div className="sec-title">
          <h2 id="admin-runs-title">Run history</h2>
        </div>
      </div>

      <form className="run-filters" aria-label="Run history filters" onSubmit={(event) => event.preventDefault()}>
        <label>
          <span>Integration</span>
          <span className="field run-filter__field">
            <select
              value={search.integration ?? ""}
              onChange={(event) => updateFilters({ integration: event.target.value || undefined })}
            >
              <option value="">All integrations</option>
              {integrationOptions.map((integration) => (
                <option key={integration.id} value={integration.id}>{integration.name}</option>
              ))}
            </select>
          </span>
        </label>
        <label>
          <span>Type</span>
          <span className="field run-filter__field">
            <select
              value={search.operation ?? ""}
              onChange={(event) =>
                updateFilters({ operation: event.target.value || undefined })
              }
            >
              <option value="">All types</option>
              {operationOptions.map(([value, label]) => (
                <option key={value} value={value}>{label}</option>
              ))}
            </select>
          </span>
        </label>
        <label>
          <span>Result</span>
          <span className="field run-filter__field">
            <select
              value={search.status ?? ""}
              onChange={(event) =>
                updateFilters({ status: (event.target.value || undefined) as IntegrationRunResultStatus | undefined })
              }
            >
              <option value="">All results</option>
              {INTEGRATION_RUN_RESULT_OPTIONS.map(([value, label]) => (
                <option key={value} value={value}>{label}</option>
              ))}
            </select>
          </span>
        </label>
      </form>

      {history.isPending ? (
        <div className="adm-state" role="status">
          Loading run history…
        </div>
      ) : history.isError ? (
        <div className="adm-state" role="alert">
          {history.error instanceof IntegrationProblem && history.error.status === 403
            ? "Admin access is required to view run history."
            : "Failed to load run history."}
        </div>
      ) : (
        <>
          {runs.length === 0 ? (
            <div className="adm-state">
              {filtered
                ? "No results match the selected filters."
                : "No integration run results have been recorded."}
            </div>
          ) : (
            <div className="run-register" aria-busy={history.isFetching}>
              <div className="run-register__head" aria-hidden="true">
                <span>Finished</span>
                <span>Integration / type</span>
                <span>Result</span>
                <span />
              </div>
              <ul className="run-register__list" aria-label="Integration run history">
                {runs.map((run) => (
                  <RunResultRow
                    key={run.id}
                    run={run}
                    integrationName={integrationNameFor(run.integration)}
                    operationName={operationNameFor(run.operation)}
                    onOpen={() => setSelectedRun(run)}
                  />
                ))}
              </ul>
            </div>
          )}
          {runs.length > 0 || search.cursor ? (
            <nav className="run-pagination" aria-label="Run history pages">
              <span role="status">{history.isPlaceholderData ? "Loading page…" : ""}</span>
              <button
                type="button"
                className="btn btn--ghost btn--sm"
                disabled={
                  (previousCursors.length === 0 && !search.cursor) || history.isPlaceholderData
                }
                onClick={previousPage}
              >
                {previousCursors.length === 0 && search.cursor ? "First page" : "Previous page"}
              </button>
              <button
                type="button"
                className="btn btn--ghost btn--sm"
                disabled={!history.data.nextCursor || history.isPlaceholderData}
                onClick={nextPage}
              >
                Next page
              </button>
            </nav>
          ) : null}
        </>
      )}

      {selectedRun ? (
        <RunDetailsModal
          run={selectedRun}
          integrationName={integrationNameFor(selectedRun.integration)}
          operationName={operationNameFor(selectedRun.operation)}
          onClose={() => setSelectedRun(null)}
        />
      ) : null}
    </section>
  );
}
