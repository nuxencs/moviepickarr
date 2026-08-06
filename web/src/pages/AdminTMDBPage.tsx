import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useCallback, useEffect, useRef } from "react";

import {
  getTMDBIntegration,
  IntegrationKeys,
  IntegrationProblem,
} from "@/api/integrations";

import { TMDBSettingsForm } from "@/components/moviepickarr/admin/TMDBSettingsForm";
import { TMDBStatus } from "@/components/moviepickarr/admin/TMDBStatus";

import { scheduledRunDiscoveryDelay } from "@/pages/tmdbRunDiscovery";

import "@/components/moviepickarr/admin/integrations.css";

const SCHEDULED_RUN_DISCOVERY_RETRY_MS = 2_000;

function useRunPolling(
  running: boolean,
  runID: number | undefined,
  refetch: () => Promise<unknown>,
  onActivityChange: () => void,
) {
  const wasRunning = useRef(false);

  useEffect(() => {
    if (!running) return;
    let interval: number | undefined;
    let fetching = false;
    const poll = async () => {
      if (fetching || document.visibilityState !== "visible") return;
      fetching = true;
      try {
        await refetch();
      } finally {
        fetching = false;
      }
    };
    const sync = () => {
      if (interval !== undefined) window.clearInterval(interval);
      interval = undefined;
      if (document.visibilityState === "visible") {
        interval = window.setInterval(() => void poll(), 2_000);
      }
    };
    const onVisibility = () => {
      sync();
      if (document.visibilityState === "visible") void poll();
    };
    sync();
    document.addEventListener("visibilitychange", onVisibility);
    return () => {
      if (interval !== undefined) window.clearInterval(interval);
      document.removeEventListener("visibilitychange", onVisibility);
    };
  }, [refetch, runID, running]);

  useEffect(() => {
    if (wasRunning.current && !running) {
      void refetch();
      onActivityChange();
    }
    wasRunning.current = running;
  }, [onActivityChange, refetch, running]);
}

function useScheduledRunDiscovery(
  running: boolean,
  nextCheckAt: string | undefined,
  refetch: () => Promise<unknown>,
  onActivityChange: () => void,
) {
  useEffect(() => {
    if (running || !nextCheckAt) return;
    const dueAt = Date.parse(nextCheckAt);
    if (Number.isNaN(dueAt)) return;
    let timeout: number | undefined;
    let fetching = false;
    let disposed = false;
    const clear = () => {
      if (timeout !== undefined) window.clearTimeout(timeout);
      timeout = undefined;
    };
    const check = async () => {
      if (fetching || document.visibilityState !== "visible") return;
      fetching = true;
      try {
        await refetch();
        onActivityChange();
      } finally {
        fetching = false;
        if (!disposed && document.visibilityState === "visible") {
          clear();
          timeout = window.setTimeout(() => void check(), SCHEDULED_RUN_DISCOVERY_RETRY_MS);
        }
      }
    };
    const schedule = () => {
      clear();
      if (document.visibilityState !== "visible") return;
      const delay = scheduledRunDiscoveryDelay(dueAt, Date.now());
      if (delay === null) {
        void check();
        return;
      }
      timeout = window.setTimeout(schedule, delay);
    };
    const onVisibility = () => {
      if (document.visibilityState !== "visible") {
        clear();
        return;
      }
      if (Date.now() >= dueAt) void check();
      else schedule();
    };
    schedule();
    document.addEventListener("visibilitychange", onVisibility);
    return () => {
      disposed = true;
      clear();
      document.removeEventListener("visibilitychange", onVisibility);
    };
  }, [nextCheckAt, onActivityChange, refetch, running]);
}

export function AdminTMDBPage() {
  const queryClient = useQueryClient();
  const config = useQuery({
    queryKey: IntegrationKeys.tmdb(),
    queryFn: ({ signal }) => getTMDBIntegration(signal),
    retry: false,
  });
  const running = config.data?.latestRun?.status === "running";
  const syncIntegrationActivity = useCallback(() => {
    void queryClient.invalidateQueries({ queryKey: IntegrationKeys.runHistory() });
  }, [queryClient]);
  useRunPolling(
    running,
    running ? config.data?.latestRun?.id : undefined,
    config.refetch,
    syncIntegrationActivity,
  );
  useScheduledRunDiscovery(
    running,
    config.data?.nextCheckAt,
    config.refetch,
    syncIntegrationActivity,
  );

  return (
    <div className="admin-section mg-rise">
      <h2 className="vis-hidden">TMDB</h2>
      {config.isPending ? (
        <div className="adm-state" role="status">
          Loading TMDB settings…
        </div>
      ) : config.isError ? (
        <div className="adm-state" role="alert">
          {config.error instanceof IntegrationProblem && config.error.status === 403
            ? "Admin access is required to view TMDB settings."
            : "TMDB settings could not be loaded."}
        </div>
      ) : (
        <>
          <TMDBStatus config={config.data} />
          <TMDBSettingsForm serverConfig={config.data} />
        </>
      )}
    </div>
  );
}
