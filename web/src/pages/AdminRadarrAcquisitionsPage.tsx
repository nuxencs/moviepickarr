import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { ChevronRightIcon, SearchIcon } from "lucide-react";
import { useDeferredValue, useMemo, useState } from "react";

import { IntegrationProblem } from "@/api/integrations";
import {
  listRadarrAcquisitions,
  RadarrKeys,
  type RadarrAcquisition,
} from "@/api/radarr";

import {
  acquisitionIsOpen,
  acquisitionTitle,
  radarrReasonLabel,
  radarrStatusLabel,
  targetName,
} from "@/components/moviepickarr/admin/radarr";
import { RadarrDisclosure } from "@/components/moviepickarr/admin/RadarrDisclosure";

function AcquisitionRow({ acquisition }: { acquisition: RadarrAcquisition }) {
  const reason = acquisition.status === "action_needed"
    ? radarrReasonLabel(acquisition.actionReason)
    : acquisition.status === "needs_preset" || acquisition.status === "needs_release"
      ? radarrReasonLabel(acquisition.actionReason)
      : undefined;
  const target = acquisition.target ?? acquisition.preset;
  const targetSummary = [targetName(target), target?.instanceName]
    .filter((value, index, values): value is string => Boolean(value) && values.indexOf(value) === index)
    .join(" · ");
  return (
    <li className="radarr-register__item">
      <Link
        to="/admin/integrations/radarr/acquisitions/$acquisitionID"
        params={{ acquisitionID: String(acquisition.id) }}
        className="radarr-acquisition-row"
      >
        <span className="radarr-acquisition-row__identity">
          <strong>{acquisitionTitle(acquisition)}</strong>
          <span>{acquisition.year ?? acquisition.identity?.year ?? "Year unavailable"}</span>
        </span>
        <span className="radarr-acquisition-row__state">
          <strong data-status={acquisition.status}>{radarrStatusLabel(acquisition.status)}</strong>
          <span>{reason ?? (acquisitionIsOpen(acquisition) ? "No Admin action required" : "Acquisition closed")}</span>
        </span>
        <span className="radarr-acquisition-row__target">
          <span>{targetSummary || "Target not selected"}</span>
        </span>
        <ChevronRightIcon aria-hidden="true" />
        <span className="vis-hidden">View acquisition details</span>
      </Link>
    </li>
  );
}

export function AdminRadarrAcquisitionsPage() {
  const acquisitions = useQuery({
    queryKey: RadarrKeys.acquisitions(),
    queryFn: ({ signal }) => listRadarrAcquisitions(signal),
    refetchInterval: () =>
      typeof document === "undefined" || document.visibilityState === "visible" ? 30_000 : false,
    refetchOnWindowFocus: true,
    refetchOnMount: "always",
    retry: false,
    staleTime: 0,
  });
  const [historySearch, setHistorySearch] = useState("");
  const deferredSearch = useDeferredValue(historySearch.trim().toLocaleLowerCase());
  const active = acquisitions.data?.filter(acquisitionIsOpen) ?? [];
  const actionRequired = active.filter((item) => ["needs_preset", "needs_release", "action_needed"].includes(item.status));
  const inProgress = active.filter((item) => !actionRequired.includes(item));
  const history = useMemo(
    () =>
      (acquisitions.data?.filter((item) => !acquisitionIsOpen(item)) ?? []).filter((item) => {
        if (!deferredSearch) return true;
        const haystack = [
          acquisitionTitle(item),
          item.status,
          item.abandonmentReason,
          targetName(item.target ?? item.preset),
        ]
          .filter(Boolean)
          .join(" ")
          .toLocaleLowerCase();
        return haystack.includes(deferredSearch);
      }),
    [acquisitions.data, deferredSearch],
  );

  return (
    <section className="radarr-page radarr-page--acquisitions mg-rise" aria-label="Radarr acquisitions">
      {acquisitions.isPending ? (
        <div className="adm-state" role="status">Loading Radarr acquisitions…</div>
      ) : acquisitions.isError ? (
        <div className="adm-state" role="alert">
          {acquisitions.error instanceof IntegrationProblem && acquisitions.error.status === 403
            ? "Admin access is required to view Radarr acquisitions."
            : "Radarr acquisitions could not be loaded."}
        </div>
      ) : (
        <>
          <div className="radarr-page__toolbar radarr-page__toolbar--overview">
            <div className="radarr-page__overview">
              <h3 id="radarr-action-required-title">Action required</h3>
              <p>Drawn winners that need an Admin decision before Radarr can continue.</p>
            </div>
            <span className="radarr-section__count" aria-label={`${actionRequired.length} ${actionRequired.length === 1 ? "acquisition requires" : "acquisitions require"} action`}>{actionRequired.length}</span>
          </div>
          <section className="radarr-queue-group" aria-labelledby="radarr-action-required-title">
            {actionRequired.length > 0 ? (
              <ul className="radarr-register" aria-label="Active Radarr acquisitions">
                {actionRequired.map((acquisition) => (
                  <AcquisitionRow key={acquisition.id} acquisition={acquisition} />
                ))}
              </ul>
            ) : (
              <p className="radarr-empty">No acquisitions need Admin action.</p>
            )}
          </section>

          {inProgress.length > 0 ? (
            <section className="radarr-queue-group" aria-labelledby="radarr-in-progress-title">
              <div className="radarr-section__head">
                <h3 id="radarr-in-progress-title">In progress</h3>
                <span className="radarr-section__count">{inProgress.length}</span>
              </div>
              <ul className="radarr-register" aria-label="Radarr acquisitions in progress">
                {inProgress.map((acquisition) => (
                  <AcquisitionRow key={acquisition.id} acquisition={acquisition} />
                ))}
              </ul>
            </section>
          ) : null}

          <RadarrDisclosure
            className="radarr-disclosure--history"
            title="History"
            meta={history.length}
          >
            <div className="radarr-acquisition-history__tools">
              <label className="field radarr-search">
                <SearchIcon aria-hidden="true" />
                <span className="vis-hidden">Search acquisition history</span>
                <input
                  type="search"
                  value={historySearch}
                  placeholder="Search history"
                  onChange={(event) => setHistorySearch(event.target.value)}
                />
              </label>
            </div>
            {history.length > 0 ? (
              <ul className="radarr-register" aria-label="Radarr acquisition history">
                {history.map((acquisition) => (
                  <AcquisitionRow key={acquisition.id} acquisition={acquisition} />
                ))}
              </ul>
            ) : (
              <p className="radarr-empty">
                {deferredSearch ? "No acquisition history matches this search." : "No acquisition history yet."}
              </p>
            )}
          </RadarrDisclosure>
        </>
      )}
    </section>
  );
}
