import { useMutation, useQueryClient } from "@tanstack/react-query";

import { IntegrationProblem } from "@/api/integrations";
import {
  confirmRadarrTarget,
  RadarrKeys,
  type RadarrAcquisition,
} from "@/api/radarr";

import {
  acquisitionPreviewReady,
  acquisitionUsesExisting,
  humanize,
  tagLabel,
  targetName,
} from "@/components/moviepickarr/admin/radarr";
import { Modal } from "@/components/moviepickarr/Modal";

function TargetFacts({ acquisition }: { acquisition: RadarrAcquisition }) {
  const target = acquisition.target ?? acquisition.preset;
  const effective = acquisition.effectiveConfig;
  const existing = acquisitionUsesExisting(acquisition);
  const selectedTags = (target?.tags ?? []).map(tagLabel).join(", ") || "None";
  const effectiveTags = (effective?.tags ?? []).map(tagLabel).join(", ") || "None";
  return (
    <dl className="radarr-review__facts">
      <div><dt>Movie</dt><dd>{acquisition.title || acquisition.identity?.title || "Untitled movie"}</dd></div>
      <div><dt>Preset</dt><dd>{targetName(target)}</dd></div>
      <div><dt>Instance</dt><dd>{target?.instanceName ?? "Not available"}</dd></div>
      <div><dt>Selected root folder</dt><dd>{target?.rootFolderPath ?? "Not available"}</dd></div>
      <div><dt>Selected quality profile</dt><dd>{target?.qualityProfileName ?? "Not available"}</dd></div>
      <div><dt>Selected tags</dt><dd>{selectedTags}</dd></div>
      <div><dt>Selected minimum availability</dt><dd>{humanize(target?.minimumAvailability ?? "unknown")}</dd></div>
      <div><dt>Mode</dt><dd>{humanize(target?.mode ?? "unknown")}</dd></div>
      {existing && effective ? (
        <>
          <div><dt>Radarr root folder</dt><dd>{effective.rootFolderPath ?? "Not available"}</dd></div>
          <div><dt>Radarr quality profile</dt><dd>{effective.qualityProfileName ?? "Not available"}</dd></div>
          <div><dt>Radarr tags</dt><dd>{effectiveTags}</dd></div>
          <div><dt>Radarr minimum availability</dt><dd>{humanize(effective.minimumAvailability ?? "unknown")}</dd></div>
          <div><dt>Radarr monitoring</dt><dd>{effective.monitored === undefined ? "Not available" : effective.monitored ? "Monitored" : "Unmonitored"}</dd></div>
          <div><dt>Existing movie</dt><dd>Use Radarr&apos;s current configuration without changes</dd></div>
        </>
      ) : null}
    </dl>
  );
}

export function RadarrTargetReviewModal({
  acquisition,
  onClose,
}: {
  acquisition: RadarrAcquisition;
  onClose: () => void;
}) {
  const queryClient = useQueryClient();
  const existing = acquisitionUsesExisting(acquisition);
  const ready = acquisitionPreviewReady(acquisition);
  const confirm = useMutation({
    mutationFn: () => confirmRadarrTarget(acquisition.id),
    onSuccess: (next) => {
      queryClient.setQueryData(RadarrKeys.acquisition(acquisition.id), next);
      void queryClient.invalidateQueries({ queryKey: RadarrKeys.acquisitions() });
      void queryClient.invalidateQueries({ queryKey: RadarrKeys.attention() });
      onClose();
    },
    onError: () => {
      void queryClient.invalidateQueries({ queryKey: RadarrKeys.acquisition(acquisition.id) });
    },
  });
  const feedback = confirm.error instanceof IntegrationProblem
    ? confirm.error.message
    : confirm.isError
      ? "The Radarr target could not be confirmed."
      : "";

  return (
    <Modal
      label="Review acquisition target"
      className="modal--radarr-review"
      capped
      dismissible={!confirm.isPending}
      onClose={onClose}
    >
      {(close) => (
        <>
          <header className="radarr-modal__head">
            <div>
              <h3>Review acquisition target</h3>
              <p>
                {existing
                  ? "This movie already exists in Radarr. Its current settings will be preserved."
                  : "Radarr will receive this exact target after confirmation."}
              </p>
            </div>
          </header>
          <div className="modal__scroll radarr-modal__scroll">
            <TargetFacts acquisition={acquisition} />
            {!ready ? (
              <p className="radarr-feedback" role="alert">
                Radarr did not finish the target preview. Choose the preset again after the connection or configuration is repaired.
              </p>
            ) : null}
            {feedback ? <p className="radarr-feedback" role="alert">{feedback}</p> : null}
          </div>
          <footer className="radarr-modal__foot">
            <button type="button" className="btn btn--ghost" disabled={confirm.isPending} onClick={close}>
              Keep editing
            </button>
            <button type="button" className="btn btn--accent" disabled={!ready || confirm.isPending} onClick={() => confirm.mutate()}>
              {confirm.isPending ? "Confirming…" : existing ? "Use existing movie" : "Add movie"}
            </button>
          </footer>
        </>
      )}
    </Modal>
  );
}
