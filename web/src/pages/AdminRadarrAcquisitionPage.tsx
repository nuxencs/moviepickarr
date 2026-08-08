import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useParams } from "@tanstack/react-router";
import { ArrowLeftIcon, BanIcon, RefreshCwIcon, SearchIcon } from "lucide-react";
import { useEffect, useState } from "react";

import { IntegrationProblem } from "@/api/integrations";
import {
  abandonRadarrAcquisition,
  getRadarrAcquisition,
  listRadarrPresets,
  RadarrKeys,
  reviewRadarrAbandonment,
  retryRadarrAcquisition,
  searchRadarrIdentity,
  selectRadarrIdentity,
  selectRadarrPreset,
  type RadarrAcquisition,
  type RadarrIdentityResult,
} from "@/api/radarr";

import {
  acquisitionPreviewReady,
  acquisitionTitle,
  acquisitionUsesExisting,
  humanize,
  radarrReasonLabel,
  radarrStatusLabel,
  tagLabel,
  targetName,
} from "@/components/moviepickarr/admin/radarr";
import { RadarrDisclosure } from "@/components/moviepickarr/admin/RadarrDisclosure";
import { RadarrReleasePicker } from "@/components/moviepickarr/admin/RadarrReleasePicker";
import { RadarrTargetReviewModal } from "@/components/moviepickarr/admin/RadarrTargetReviewModal";
import { Modal } from "@/components/moviepickarr/Modal";

function mutationMessage(error: unknown, fallback: string) {
  return error instanceof IntegrationProblem ? error.message : fallback;
}

function IdentityResolver({ acquisition }: { acquisition: RadarrAcquisition }) {
  const queryClient = useQueryClient();
  const [query, setQuery] = useState(`${acquisitionTitle(acquisition)} ${acquisition.year ?? ""}`.trim());
  const [feedback, setFeedback] = useState("");
  const search = useMutation({
    mutationFn: () => searchRadarrIdentity(acquisition.id, query.trim()),
    onSuccess: () => setFeedback(""),
    onError: (error) => setFeedback(mutationMessage(error, "Radarr identity search failed.")),
  });
  const select = useMutation({
    mutationFn: (result: RadarrIdentityResult) => selectRadarrIdentity(acquisition.id, result.tmdbId),
    onSuccess: (next) => {
      queryClient.setQueryData(RadarrKeys.acquisition(acquisition.id), next);
      void queryClient.invalidateQueries({ queryKey: RadarrKeys.acquisitions() });
      setFeedback("");
    },
    onError: (error) => setFeedback(mutationMessage(error, "The movie identity could not be selected.")),
  });
  const busy = search.isPending || select.isPending;

  return (
    <section className="radarr-detail__section" aria-labelledby="radarr-identity-title">
      <div className="radarr-detail__section-head">
        <div>
          <h3 id="radarr-identity-title">Confirm identity</h3>
          <p>TMDB and IMDb could not identify this movie exactly. Search Radarr and choose the correct result.</p>
        </div>
      </div>
      <form
        className="radarr-identity-search"
        onSubmit={(event) => {
          event.preventDefault();
          if (query.trim()) search.mutate();
        }}
      >
        <label className="field">
          <SearchIcon aria-hidden="true" />
          <span className="vis-hidden">Movie title and year</span>
          <input value={query} onChange={(event) => setQuery(event.target.value)} />
        </label>
        <button type="submit" className="btn btn--ghost" disabled={busy || !query.trim()}>
          {search.isPending ? "Searching…" : "Search Radarr"}
        </button>
      </form>
      {feedback ? <p className="radarr-feedback" role="alert">{feedback}</p> : null}
      {search.data ? (
        search.data.length > 0 ? (
          <ul className="radarr-identity-results" aria-label="Radarr movie matches">
            {search.data.map((result) => (
              <li key={result.tmdbId}>
                <div><strong>{result.title}</strong><span>{result.year ?? "Year unavailable"} · TMDB {result.tmdbId}</span></div>
                <button type="button" className="btn btn--ghost btn--sm" disabled={busy} onClick={() => select.mutate(result)}>
                  {select.isPending ? "Selecting…" : "Use this movie"}
                </button>
              </li>
            ))}
          </ul>
        ) : <p className="radarr-empty">No matching Radarr movies were found.</p>
      ) : null}
    </section>
  );
}

function LockedIdentityRecovery({ acquisition }: { acquisition: RadarrAcquisition }) {
  const target = acquisition.target ?? acquisition.preset;
  const instanceName = target?.instanceName ?? "the selected Radarr instance";

  return (
    <section className="radarr-detail__section" aria-labelledby="radarr-identity-title">
      <div className="radarr-detail__section-head">
        <div>
          <h3 id="radarr-identity-title">Restore the Radarr movie</h3>
          <p>Restore the exact movie in {instanceName}, or abandon this acquisition.</p>
        </div>
      </div>
    </section>
  );
}

function TargetFacts({ acquisition }: { acquisition: RadarrAcquisition }) {
  const target = acquisition.target ?? acquisition.preset;
  const effective = acquisition.effectiveConfig;
  const existing = acquisitionUsesExisting(acquisition);
  const selectedTags = (target?.tags ?? []).map(tagLabel).join(", ") || "None";
  const effectiveTags = (effective?.tags ?? []).map(tagLabel).join(", ") || "None";
  return (
    <dl className="radarr-detail__facts">
      <div><dt>Preset</dt><dd>{targetName(target)}</dd></div>
      <div><dt>Instance</dt><dd>{target?.instanceName ?? "Not selected"}</dd></div>
      <div><dt>Selected root folder</dt><dd>{target?.rootFolderPath ?? "Not selected"}</dd></div>
      <div><dt>Selected quality profile</dt><dd>{target?.qualityProfileName ?? "Not selected"}</dd></div>
      <div><dt>Selected tags</dt><dd>{selectedTags}</dd></div>
      <div><dt>Selected minimum availability</dt><dd>{humanize(target?.minimumAvailability ?? "unknown")}</dd></div>
      <div><dt>Mode</dt><dd>{humanize(target?.mode ?? "unknown")}</dd></div>
      <div><dt>Monitoring</dt><dd>{effective?.monitored === undefined ? "Managed by workflow" : effective.monitored ? "Monitored" : "Unmonitored"}</dd></div>
      {existing && effective ? (
        <>
          <div><dt>Radarr root folder</dt><dd>{effective.rootFolderPath ?? "Not available"}</dd></div>
          <div><dt>Radarr quality profile</dt><dd>{effective.qualityProfileName ?? "Not available"}</dd></div>
          <div><dt>Radarr tags</dt><dd>{effectiveTags}</dd></div>
          <div><dt>Radarr minimum availability</dt><dd>{humanize(effective.minimumAvailability ?? "unknown")}</dd></div>
        </>
      ) : null}
    </dl>
  );
}

function TargetSummary({ acquisition }: { acquisition: RadarrAcquisition }) {
  const target = acquisition.target ?? acquisition.preset;
  const effective = acquisition.effectiveConfig;
  const quality = acquisitionUsesExisting(acquisition)
    ? effective?.qualityProfileName ?? target?.qualityProfileName
    : target?.qualityProfileName;
  const parts = [
    targetName(target),
    target?.instanceName,
    quality,
    target?.mode ? humanize(target.mode) : undefined,
  ].filter((value, index, values): value is string => Boolean(value) && values.indexOf(value) === index);
  return <p className="radarr-target-summary">{parts.join(" · ")}</p>;
}

function AbandonModal({ acquisition, onClose }: { acquisition: RadarrAcquisition; onClose: () => void }) {
  const queryClient = useQueryClient();
  const [reason, setReason] = useState("");
  const review = useQuery({
    queryKey: RadarrKeys.abandonmentReview(acquisition.id),
    queryFn: ({ signal }) => reviewRadarrAbandonment(acquisition.id, signal),
    refetchOnWindowFocus: false,
    retry: false,
    staleTime: 0,
  });
  const reviewedAcquisition = review.data?.acquisition ?? acquisition;
  const activity = review.data?.activity;
  const complete = activity === "complete" || reviewedAcquisition.status === "downloaded";
  useEffect(() => {
    if (!review.data) return;
    queryClient.setQueryData(RadarrKeys.acquisition(acquisition.id), review.data.acquisition);
    void queryClient.invalidateQueries({ queryKey: RadarrKeys.acquisitions(), exact: true });
    void queryClient.invalidateQueries({ queryKey: RadarrKeys.attention() });
  }, [acquisition.id, queryClient, review.data]);
  const abandon = useMutation({
    mutationFn: () => abandonRadarrAcquisition(acquisition.id, reason.trim(), activity),
    onSuccess: (next) => {
      queryClient.setQueryData(RadarrKeys.acquisition(acquisition.id), next);
      void queryClient.invalidateQueries({ queryKey: RadarrKeys.acquisitions() });
      void queryClient.invalidateQueries({ queryKey: RadarrKeys.attention() });
      onClose();
    },
    onError: () => {
      void review.refetch();
    },
  });
  const feedback = abandon.isError
    ? mutationMessage(abandon.error, "The acquisition could not be abandoned.")
    : "";

  return (
    <Modal label="Abandon acquisition?" className="modal--form" dismissible={!abandon.isPending} onClose={onClose}>
      {(close) => (
        <form
          className="adm-sheet"
          onSubmit={(event) => {
            event.preventDefault();
            if (reason.trim()) abandon.mutate();
          }}
        >
          <h3 className="adm-modal__title">Abandon acquisition?</h3>
          <p className="adm-modal__sub">
            Moviepickarr will stop tracking this acquisition. It will not delete or change anything in Radarr.
          </p>
          {review.isPending ? (
            <p className="radarr-action-feedback" role="status">Checking current Radarr activity…</p>
          ) : null}
          {activity === "active" ? (
            <p className="radarr-warning">Radarr still has active work for this movie. That work will continue after abandonment.</p>
          ) : null}
          {activity === "unavailable" || review.isError ? (
            <p className="radarr-warning">Moviepickarr could not verify current Radarr activity. Work in Radarr may continue after abandonment.</p>
          ) : null}
          {complete ? (
            <p className="radarr-action-feedback" role="status">Radarr now reports a file for this movie. This acquisition is complete and cannot be abandoned.</p>
          ) : (
            <>
              <label className="fieldgroup radarr-abandon-reason">
                <span>Reason</span>
                <span className="field radarr-textarea">
                  <textarea
                    required
                    rows={4}
                    value={reason}
                    placeholder="Why is this acquisition being abandoned?"
                    onChange={(event) => setReason(event.target.value)}
                  />
                </span>
              </label>
              {feedback ? <p className="radarr-feedback" role="alert">{feedback}</p> : null}
              <div className="adm-modal__actions">
                <button type="button" className="btn btn--ghost" disabled={abandon.isPending} onClick={close}>Keep acquisition</button>
                <button type="submit" className="btn btn--danger" disabled={review.isPending || abandon.isPending || !reason.trim()}>
                  {abandon.isPending ? "Abandoning…" : "Abandon acquisition"}
                </button>
              </div>
            </>
          )}
          {complete ? (
            <div className="adm-modal__actions">
              <button type="button" className="btn btn--ghost" onClick={close}>Close</button>
            </div>
          ) : null}
        </form>
      )}
    </Modal>
  );
}

export function AdminRadarrAcquisitionPage() {
  const { acquisitionID } = useParams({
    from: "/_app/admin/integrations/radarr/acquisitions/$acquisitionID",
  });
  const queryClient = useQueryClient();
  const acquisition = useQuery({
    queryKey: RadarrKeys.acquisition(acquisitionID),
    queryFn: ({ signal }) => getRadarrAcquisition(acquisitionID, signal),
    refetchInterval: () => document.visibilityState === "visible" ? 10_000 : false,
    refetchOnWindowFocus: true,
    retry: false,
    staleTime: 0,
  });
  const presets = useQuery({
    queryKey: RadarrKeys.presets(),
    queryFn: ({ signal }) => listRadarrPresets(signal),
    retry: false,
  });
  const [presetID, setPresetID] = useState("");
  const [review, setReview] = useState(false);
  const [releasePicker, setReleasePicker] = useState(false);
  const [abandon, setAbandon] = useState(false);
  const [feedback, setFeedback] = useState("");
  const selectedPresetID = acquisition.data?.target?.presetId ?? acquisition.data?.preset?.presetId;
  useEffect(() => {
    if (selectedPresetID !== undefined) setPresetID(String(selectedPresetID));
  }, [selectedPresetID]);
  const choosePreset = useMutation({
    mutationFn: () => selectRadarrPreset(acquisitionID, Number(presetID)),
    onSuccess: (next) => {
      queryClient.setQueryData(RadarrKeys.acquisition(acquisitionID), next);
      void queryClient.invalidateQueries({ queryKey: RadarrKeys.acquisitions() });
      setFeedback("");
      const nextLocked = next.targetLocked ?? Boolean(next.radarrMovieId);
      setReview(!nextLocked && acquisitionPreviewReady(next));
    },
    onError: (error) => setFeedback(mutationMessage(error, "The preset could not be selected.")),
  });
  const retry = useMutation({
    mutationFn: () => retryRadarrAcquisition(acquisitionID),
    onSuccess: (next) => {
      queryClient.setQueryData(RadarrKeys.acquisition(acquisitionID), next);
      void queryClient.invalidateQueries({ queryKey: RadarrKeys.acquisitions() });
      void queryClient.invalidateQueries({ queryKey: RadarrKeys.attention() });
      setFeedback("");
    },
    onError: (error) => setFeedback(mutationMessage(error, "The Radarr action could not be retried.")),
  });

  if (acquisition.isPending) return <div className="adm-state" role="status">Loading acquisition…</div>;
  if (acquisition.isError) {
    return (
      <div className="adm-state" role="alert">
        {acquisition.error instanceof IntegrationProblem && acquisition.error.status === 403
          ? "Admin access is required to view this acquisition."
          : acquisition.error instanceof IntegrationProblem && acquisition.error.status === 404
            ? "This acquisition no longer exists."
            : "The Radarr acquisition could not be loaded."}
      </div>
    );
  }

  const item = acquisition.data;
  const target = item.target ?? item.preset;
  const locked = item.targetLocked ?? Boolean(item.radarrMovieId);
  const open = item.status !== "downloaded" && item.status !== "abandoned";
  const checkingAdd = !locked && item.mutationState === "adding";
  const checkingGrab = locked && item.mutationState === "grabbing";
  const reason = item.status === "action_needed" || item.status === "needs_preset" || item.status === "needs_release"
    ? radarrReasonLabel(item.actionReason)
    : undefined;
  const actionFailure = item.status === "action_needed"
    ? item.latestFailure ?? (item.actionMessage !== reason ? item.actionMessage : undefined)
    : undefined;
  const validPresets = (presets.data ?? []).filter((preset) => !preset.archivedAt && preset.valid !== false);
  const canSearchRelease = locked && open && !item.activeQueue && (
    item.status === "needs_release" ||
    (item.status === "action_needed" && [
      "release_required",
      "no_releases",
      "release_failed",
      "import_failed",
    ].includes(item.actionReason ?? ""))
  );
  const canRetryLockedAction = locked && item.status === "action_needed" &&
    item.actionReason !== "identity_required" && item.actionReason !== "release_required";
  const retryLabel = retry.isPending
    ? checkingAdd || checkingGrab ? "Checking…" : "Retrying…"
    : checkingAdd ? "Check Radarr add"
      : checkingGrab ? "Check Radarr status"
        : "Retry Radarr action";
  const historicalFailure = item.status !== "action_needed" ? item.latestFailure : undefined;
  const recordVisible = Boolean(item.abandonmentReason || historicalFailure);
  const recordMeta = item.abandonmentReason ? "Reason recorded" : "Previous failure";

  return (
    <article className="radarr-page radarr-detail mg-rise" aria-labelledby="radarr-acquisition-title">
      <Link to="/admin/integrations/radarr" className="radarr-back">
        <ArrowLeftIcon aria-hidden="true" />
        All acquisitions
      </Link>
      <header className="radarr-detail__hero">
        <div>
          <h2 id="radarr-acquisition-title">{acquisitionTitle(item)}</h2>
          <p>{item.year ?? item.identity?.year ?? "Year unavailable"}</p>
        </div>
        <div className="radarr-detail__hero-actions">
          <span className="radarr-status" data-status={item.status}>{radarrStatusLabel(item.status)}</span>
          {open ? (
            <button type="button" className="iconbtn iconbtn--danger" title="Abandon acquisition" aria-label="Abandon acquisition" onClick={() => setAbandon(true)}>
              <BanIcon aria-hidden="true" />
            </button>
          ) : null}
        </div>
      </header>
      {reason ? <p className="radarr-detail__reason">{reason}</p> : null}
      {actionFailure ? <p className="radarr-feedback" role="alert">{actionFailure}</p> : null}
      {feedback ? <p className="radarr-feedback" role="alert">{feedback}</p> : null}
      {target ? <TargetSummary acquisition={item} /> : null}

      {checkingAdd && open ? (
        <section className="radarr-current-action" aria-labelledby="radarr-check-add-title">
          <div>
            <h3 id="radarr-check-add-title">Confirm the Radarr add</h3>
            <p>Radarr may have accepted this movie. Check the result before changing the target.</p>
          </div>
          <button type="button" className="iconbtn" title={retryLabel} aria-label={retryLabel} disabled={retry.isPending} onClick={() => retry.mutate()}>
            <RefreshCwIcon className={retry.isPending ? "animate-spin mg-spin" : undefined} aria-hidden="true" />
          </button>
        </section>
      ) : !locked && open ? (
        <section className="radarr-current-action radarr-current-action--form" aria-labelledby="radarr-target-title">
          <div>
            <h3 id="radarr-target-title">Choose target</h3>
            <p>The target can change until Radarr adds or adopts the movie.</p>
          </div>
          {validPresets.length > 0 ? (
            <form
              className="radarr-target-select"
              onSubmit={(event) => {
                event.preventDefault();
                if (presetID) choosePreset.mutate();
              }}
            >
              <label className="field">
                <span className="vis-hidden">Acquisition preset</span>
                <select value={presetID} onChange={(event) => setPresetID(event.target.value)}>
                  <option value="">Choose a preset</option>
                  {validPresets.map((preset) => <option key={preset.id} value={String(preset.id)}>{preset.name}</option>)}
                </select>
              </label>
              <button type="submit" className="btn btn--accent" disabled={!presetID || choosePreset.isPending}>
                {choosePreset.isPending ? "Preparing…" : "Review target"}
              </button>
            </form>
          ) : presets.isPending ? (
            <p className="radarr-empty">Loading presets…</p>
          ) : presets.isError ? (
            <p className="radarr-feedback" role="alert">Acquisition presets could not be loaded.</p>
          ) : (
            <p className="radarr-empty">No valid presets are available. <Link to="/admin/integrations/radarr/setup">Create a preset</Link>.</p>
          )}
        </section>
      ) : null}

      {item.actionReason === "identity_required"
        ? locked ? <LockedIdentityRecovery acquisition={item} /> : <IdentityResolver acquisition={item} />
        : null}

      {canSearchRelease ? (
        <section className="radarr-current-action" aria-labelledby="radarr-release-action-title">
          <div>
            <h3 id="radarr-release-action-title">File required</h3>
            <p>The movie exists in Radarr, but the selected target does not report a file.</p>
          </div>
          <button type="button" className="btn btn--accent" onClick={() => setReleasePicker(true)}><SearchIcon aria-hidden="true" />Search releases</button>
        </section>
      ) : null}

      {!checkingAdd && canRetryLockedAction ? (
        <section className="radarr-current-action" aria-label="Retry Radarr action">
          <div><h3>Radarr needs another check</h3><p>Retry the last Radarr action after the issue is resolved.</p></div>
          <button type="button" className="iconbtn" title={retryLabel} aria-label={retryLabel} disabled={retry.isPending} onClick={() => retry.mutate()}>
            <RefreshCwIcon className={retry.isPending ? "animate-spin mg-spin" : undefined} aria-hidden="true" />
          </button>
        </section>
      ) : null}

      {target ? (
        <RadarrDisclosure title="Target details" meta={targetName(target)}>
          <TargetFacts acquisition={item} />
        </RadarrDisclosure>
      ) : null}

      {item.latestRelease ? (
        <RadarrDisclosure
          title="Selected release"
          meta={item.latestRelease.quality ?? "Unknown quality"}
        >
          <dl className="radarr-detail__facts">
            <div><dt>Release</dt><dd>{item.latestRelease.title ?? "Not available"}</dd></div>
            <div><dt>Quality</dt><dd>{item.latestRelease.quality ?? "Not available"}</dd></div>
            {typeof item.manualAttemptCount === "number" && item.manualAttemptCount > 0 ? (
              <div><dt>Total manual release attempts</dt><dd>{item.manualAttemptCount}</dd></div>
            ) : null}
          </dl>
        </RadarrDisclosure>
      ) : null}

      {recordVisible ? (
        <RadarrDisclosure title="Acquisition record" meta={recordMeta}>
          <dl className="radarr-detail__facts">
            {item.abandonmentReason ? (
              <div><dt>Abandonment reason</dt><dd>{item.abandonmentReason}</dd></div>
            ) : null}
            {historicalFailure ? (
              <div><dt>Latest recorded failure</dt><dd>{historicalFailure}</dd></div>
            ) : null}
          </dl>
        </RadarrDisclosure>
      ) : null}

      {review ? <RadarrTargetReviewModal acquisition={item} onClose={() => setReview(false)} /> : null}
      {releasePicker ? <RadarrReleasePicker acquisition={item} onClose={() => setReleasePicker(false)} /> : null}
      {abandon ? <AbandonModal acquisition={item} onClose={() => setAbandon(false)} /> : null}
    </article>
  );
}
