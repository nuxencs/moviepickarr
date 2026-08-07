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
  acquisitionUpdatedAt,
  radarrReasonLabel,
  radarrStatusLabel,
  targetName,
  timestampLabel,
} from "@/components/moviepickarr/admin/radarr";

function AcquisitionRow({ acquisition }: { acquisition: RadarrAcquisition }) {
  const reason = acquisition.status === "action_needed"
    ? radarrReasonLabel(acquisition.actionReason)
    : acquisition.status === "needs_preset" || acquisition.status === "needs_release"
      ? radarrReasonLabel(acquisition.actionReason)
      : undefined;
  const updated = acquisitionUpdatedAt(acquisition);
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
          <span>{reason ?? targetName(acquisition.target ?? acquisition.preset)}</span>
        </span>
        <span className="radarr-acquisition-row__target">
          <span>{targetName(acquisition.target ?? acquisition.preset)}</span>
          {updated ? <time dateTime={updated}>{timestampLabel(updated)}</time> : null}
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
    <section className="radarr-page" aria-labelledby="radarr-acquisitions-title">
      <div className="sec-head radarr-page__head">
        <div className="sec-title">
          <h2 id="radarr-acquisitions-title">Acquisitions</h2>
          {acquisitions.data ? (
            <span className="sec-count">{active.length} active</span>
          ) : null}
        </div>
      </div>

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
          <section className="radarr-section" aria-labelledby="radarr-active-title">
            <div className="radarr-section__head">
              <div>
                <h3 id="radarr-active-title">Needs attention</h3>
                <p>Every revealed draw stays here until Radarr imports a file or an Admin abandons it.</p>
              </div>
              <span className="radarr-section__count">{active.length}</span>
            </div>
            {active.length > 0 ? (
              <ul className="radarr-register" aria-label="Active Radarr acquisitions">
                {active.map((acquisition) => (
                  <AcquisitionRow key={acquisition.id} acquisition={acquisition} />
                ))}
              </ul>
            ) : (
              <p className="radarr-empty">No acquisitions need attention.</p>
            )}
          </section>

          <section className="radarr-section" aria-labelledby="radarr-history-title">
            <div className="radarr-section__head radarr-section__head--controls">
              <div>
                <h3 id="radarr-history-title">History</h3>
                <p>Downloaded and abandoned acquisitions remain with the movie.</p>
              </div>
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
          </section>
        </>
      )}
    </section>
  );
}
