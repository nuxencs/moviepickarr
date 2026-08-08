import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { RefreshCwIcon, SparklesIcon, XIcon } from "lucide-react";
import { useState } from "react";

import {
  cancelIntegrationRun,
  IntegrationKeys,
  IntegrationProblem,
  startTMDBRun,
} from "@/api/integrations";
import type {
  IntegrationRun,
  IntegrationState,
  TMDBIntegration,
} from "@/api/integrations";

import {
  INTEGRATION_RUN_STATUS_LABELS,
  integrationRunOperationLabel,
} from "@/components/moviepickarr/admin/integrationRunLabels";
import { Modal } from "@/components/moviepickarr/Modal";

const STATE_LABELS: Record<IntegrationState, string> = {
  disabled: "Disabled",
  connected: "Connected",
  could_not_verify: "Could not verify",
  error: "Error",
  credential_unavailable: "Credential unavailable",
};

const MONTH_FORMATTER = new Intl.DateTimeFormat("en-US", {
  month: "short",
  timeZone: "UTC",
});

const TIME_FORMATTER = new Intl.DateTimeFormat("en-US", {
  hour: "numeric",
  minute: "2-digit",
  timeZone: "UTC",
});

function timestampLabel(iso: string) {
  const value = new Date(iso);
  if (Number.isNaN(value.getTime())) return "Recorded";
  const month = MONTH_FORMATTER.format(value);
  const time = TIME_FORMATTER.format(value);
  return `${month} ${value.getUTCDate()}, ${value.getUTCFullYear()} at ${time} UTC`;
}

function Timestamp({ value }: { value?: string }) {
  if (!value) return <>Not available</>;
  return <time dateTime={value}>{timestampLabel(value)}</time>;
}

function RunSummary({ run }: { run?: IntegrationRun }) {
  if (!run) return <>Not available</>;
  const totalLabel =
    run.progress.total > 0
      ? `${run.progress.processed} of ${run.progress.total} processed`
      : `${run.progress.processed} processed`;
  return (
    <span className="int-run-summary">
      <span>
        {INTEGRATION_RUN_STATUS_LABELS[run.status]} · {integrationRunOperationLabel(run.operation)}
      </span>
      <span>{totalLabel}</span>
      <span>{run.progress.remaining} remaining</span>
    </span>
  );
}

function actionMessage(error: unknown) {
  if (!(error instanceof IntegrationProblem)) return "The TMDB action could not be completed.";
  if (error.title === "run_active") return "A TMDB library run is already active.";
  if (error.title === "integration_unavailable") return "TMDB is not available for new work.";
  if (error.title === "run_not_active") return "This run is no longer active.";
  return error.message;
}

export function TMDBStatus({ config }: { config: TMDBIntegration }) {
  const queryClient = useQueryClient();
  const [feedback, setFeedback] = useState("");
  const [confirmReEnrich, setConfirmReEnrich] = useState(false);
  const activeRun = config.latestRun?.status === "running" ? config.latestRun : undefined;
  const running = activeRun?.operation !== "enrich_movie" ? activeRun : undefined;
  const available = config.state === "connected" || config.state === "could_not_verify";

  const start = useMutation({
    mutationFn: ({ operation, confirm }: { operation: "refresh_stale" | "re_enrich_all"; confirm: boolean }) =>
      startTMDBRun(operation, confirm),
    onSuccess: (result, variables) => {
      setConfirmReEnrich(false);
      if ("noWork" in result) {
        setFeedback(
          variables.operation === "refresh_stale"
            ? "No missing or stale movies were found."
            : "No movies were available to re-enrich.",
        );
        void queryClient.invalidateQueries({ queryKey: IntegrationKeys.tmdb() });
      } else {
        setFeedback("TMDB run started.");
        queryClient.setQueryData<TMDBIntegration>(IntegrationKeys.tmdb(), (current) =>
          current ? { ...current, latestRun: result } : current,
        );
      }
      void queryClient.invalidateQueries({ queryKey: IntegrationKeys.runHistory() });
    },
    onError: (error) => {
      setFeedback(actionMessage(error));
      void queryClient.invalidateQueries({ queryKey: IntegrationKeys.tmdb() });
      void queryClient.invalidateQueries({ queryKey: IntegrationKeys.runHistory() });
    },
  });
  const cancel = useMutation({
    mutationFn: cancelIntegrationRun,
    onSuccess: () => {
      setFeedback("Cancellation requested. The active request will finish first.");
      void queryClient.invalidateQueries({ queryKey: IntegrationKeys.tmdb() });
      void queryClient.invalidateQueries({ queryKey: IntegrationKeys.runHistory() });
    },
    onError: (error) => {
      setFeedback(actionMessage(error));
      void queryClient.invalidateQueries({ queryKey: IntegrationKeys.tmdb() });
      void queryClient.invalidateQueries({ queryKey: IntegrationKeys.runHistory() });
    },
  });
  const busy = start.isPending || cancel.isPending;

  return (
    <section className="int-status" aria-label="TMDB status">
      <div className="int-status__head">
        <h3 className="int-state" data-state={config.state}>
          {STATE_LABELS[config.state]}
        </h3>
        <p className="int-status__last-success">
          {config.lastSuccessfulRunAt ? (
            <>
              Last refresh succeeded <Timestamp value={config.lastSuccessfulRunAt} />
            </>
          ) : (
            <>No successful refresh recorded</>
          )}
        </p>
      </div>
      {config.reason ? <p className="int-status__reason">{config.reason}</p> : null}
      {config.warnings && config.warnings.length > 0 ? (
        <div className="int-effective-warning" role="alert" aria-label="Configuration warnings">
          <ul>
            {config.warnings.map((warning) => (
              <li key={`${warning.field}:${warning.message}`}>{warning.message}</li>
            ))}
          </ul>
        </div>
      ) : null}
      {config.latestRun?.errorSummary ? (
        <p className="int-status__run-error" role="alert" aria-label="Latest run error">
          {config.latestRun.errorSummary}
        </p>
      ) : null}
      {activeRun ? (
        <section className="int-status__active" aria-label="Active run">
          <h4>Active run</h4>
          <RunSummary run={activeRun} />
        </section>
      ) : null}
      <details className="int-status__activity" aria-label="Activity details">
        <summary>Activity details</summary>
        <dl className="int-status__facts">
          <div>
            <dt>Last library scan</dt>
            <dd>
              <Timestamp value={config.lastCheckedAt} />
            </dd>
          </div>
          <div>
            <dt>Last connection test</dt>
            <dd>
              <Timestamp value={config.lastConnectionTestedAt} />
            </dd>
          </div>
          <div>
            <dt>Next scheduled scan</dt>
            <dd>
              <Timestamp value={config.nextCheckAt} />
            </dd>
          </div>
          <div>
            <dt>Last successful refresh</dt>
            <dd>
              <Timestamp value={config.lastSuccessfulRunAt} />
            </dd>
          </div>
          <div>
            <dt>Latest run</dt>
            <dd>
              {activeRun ? <>In progress above</> : <RunSummary run={config.latestRun} />}
            </dd>
          </div>
        </dl>
      </details>

      <div className="int-status__actions">
        <button
          type="button"
          className="btn btn--ghost btn--sm"
          disabled={!available || Boolean(running) || busy}
          onClick={() => {
            setFeedback("");
            start.mutate({ operation: "refresh_stale", confirm: false });
          }}
        >
          <RefreshCwIcon aria-hidden="true" />
          Refresh stale now
        </button>
        <button
          type="button"
          className="btn btn--ghost btn--sm"
          disabled={!available || Boolean(running) || busy}
          onClick={() => setConfirmReEnrich(true)}
        >
          <SparklesIcon aria-hidden="true" />
          Re-enrich all
        </button>
        {running ? (
          <button
            type="button"
            className="btn btn--danger btn--sm"
            disabled={busy}
            onClick={() => cancel.mutate(running.id)}
          >
            <XIcon aria-hidden="true" />
            {cancel.isPending ? "Cancelling…" : "Cancel run"}
          </button>
        ) : null}
        <Link
          to="/admin/runs"
          search={{ integration: "tmdb" }}
          className="int-history-link"
        >
          View run history
        </Link>
      </div>
      {feedback ? (
        <p className="int-action-feedback" role="status">
          {feedback}
        </p>
      ) : null}

      {confirmReEnrich ? (
        <Modal
          label="Re-enrich every movie?"
          className="modal--form"
          dismissible={!start.isPending}
          onClose={() => setConfirmReEnrich(false)}
        >
          {(close) => (
            <div className="adm-sheet">
              <h3 className="adm-modal__title">Re-enrich every movie?</h3>
              <p className="adm-modal__sub">
                This schedules every movie for TMDB enrichment. Existing metadata stays available
                while the run works through the library.
              </p>
              <div className="adm-modal__actions">
                <button
                  type="button"
                  className="btn btn--ghost"
                  disabled={start.isPending}
                  onClick={close}
                >
                  Cancel
                </button>
                <button
                  type="button"
                  className="btn btn--accent"
                  disabled={start.isPending}
                  onClick={() => start.mutate({ operation: "re_enrich_all", confirm: true })}
                >
                  {start.isPending ? "Starting…" : "Re-enrich all"}
                </button>
              </div>
            </div>
          )}
        </Modal>
      ) : null}
    </section>
  );
}
