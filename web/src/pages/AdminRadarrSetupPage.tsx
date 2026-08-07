import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { PlusIcon } from "lucide-react";
import { useState } from "react";

import { IntegrationKeys, IntegrationProblem } from "@/api/integrations";
import {
  archiveRadarrInstance,
  archiveRadarrPreset,
  listRadarrInstances,
  listRadarrPresets,
  RadarrKeys,
  type RadarrInstance,
  type RadarrPreset,
} from "@/api/radarr";

import { humanize, timestampLabel } from "@/components/moviepickarr/admin/radarr";
import { RadarrInstanceDialog } from "@/components/moviepickarr/admin/RadarrInstanceDialog";
import { RadarrPresetDialog } from "@/components/moviepickarr/admin/RadarrPresetDialog";
import { Modal } from "@/components/moviepickarr/Modal";

type SetupDialog =
  | { kind: "instance"; value?: RadarrInstance }
  | { kind: "preset"; value?: RadarrPreset }
  | { kind: "archive-instance"; value: RadarrInstance }
  | { kind: "archive-preset"; value: RadarrPreset }
  | null;

function ArchiveSetupDialog({
  dialog,
  onClose,
  onDone,
}: {
  dialog: Extract<SetupDialog, { kind: "archive-instance" | "archive-preset" }>;
  onClose: () => void;
  onDone: () => void;
}) {
  const isInstance = dialog.kind === "archive-instance";
  const archive = useMutation({
    mutationFn: () => isInstance
      ? archiveRadarrInstance(dialog.value.id)
      : archiveRadarrPreset(dialog.value.id),
    onSuccess: onDone,
  });
  const feedback = archive.isError
    ? archive.error instanceof IntegrationProblem
      ? archive.error.message
      : `The ${isInstance ? "instance" : "preset"} could not be archived.`
    : "";
  const title = `Archive ${dialog.value.name}?`;
  return (
    <Modal label={title} className="modal--form" dismissible={!archive.isPending} onClose={onClose}>
      {(close) => (
        <div className="adm-sheet">
          <h3 className="adm-modal__title">{title}</h3>
          <p className="adm-modal__sub">
            {isInstance
              ? "This instance and its presets will be archived. It cannot be archived while an unresolved acquisition targets it. Existing history keeps its snapshots."
              : "This preset will disappear from future target selection. Existing acquisition snapshots remain unchanged."}
          </p>
          {feedback ? <p className="radarr-feedback" role="alert">{feedback}</p> : null}
          <div className="adm-modal__actions">
            <button type="button" className="btn btn--ghost" disabled={archive.isPending} onClick={close}>Keep {isInstance ? "instance" : "preset"}</button>
            <button type="button" className="btn btn--danger" disabled={archive.isPending} onClick={() => archive.mutate()}>{archive.isPending ? "Archiving…" : `Archive ${isInstance ? "instance" : "preset"}`}</button>
          </div>
        </div>
      )}
    </Modal>
  );
}

export function AdminRadarrSetupPage() {
  const queryClient = useQueryClient();
  const instances = useQuery({
    queryKey: RadarrKeys.instances(),
    queryFn: ({ signal }) => listRadarrInstances(signal),
    retry: false,
  });
  const presets = useQuery({
    queryKey: RadarrKeys.presets(),
    queryFn: ({ signal }) => listRadarrPresets(signal),
    retry: false,
  });
  const [dialog, setDialog] = useState<SetupDialog>(null);
  const activeInstances = instances.data?.filter((item) => !item.archivedAt) ?? [];
  const archivedInstances = instances.data?.filter((item) => item.archivedAt) ?? [];
  const activePresets = presets.data?.filter((item) => !item.archivedAt) ?? [];
  const archivedPresets = presets.data?.filter((item) => item.archivedAt) ?? [];
  const reload = () => {
    setDialog(null);
    void queryClient.invalidateQueries({ queryKey: RadarrKeys.instances() });
    void queryClient.invalidateQueries({ queryKey: RadarrKeys.presets() });
    void queryClient.invalidateQueries({ queryKey: IntegrationKeys.list() });
  };
  const error = instances.error ?? presets.error;

  return (
    <section className="radarr-page" aria-labelledby="radarr-setup-title">
      <div className="sec-head radarr-page__head"><div className="sec-title"><h2 id="radarr-setup-title">Setup</h2></div></div>
      {instances.isPending || presets.isPending ? (
        <div className="adm-state" role="status">Loading Radarr setup…</div>
      ) : error ? (
        <div className="adm-state" role="alert">
          {error instanceof IntegrationProblem && error.status === 403
            ? "Admin access is required to manage Radarr setup."
            : "Radarr setup could not be loaded."}
        </div>
      ) : (
        <>
          <section className="radarr-section" aria-labelledby="radarr-instances-title">
            <div className="radarr-section__head radarr-section__head--controls">
              <div><h3 id="radarr-instances-title">Instances</h3><p>Each connection represents a media variant or collection boundary.</p></div>
              <button type="button" className="btn btn--ghost btn--sm" onClick={() => setDialog({ kind: "instance" })}><PlusIcon aria-hidden="true" />Add instance</button>
            </div>
            {activeInstances.length > 0 ? (
              <ul className="radarr-setup-list" aria-label="Radarr instances">
                {activeInstances.map((instance) => (
                  <li key={instance.id}>
                    <span className="radarr-setup-list__identity"><strong>{instance.name}</strong><span>{instance.url ?? "URL stored securely"}</span></span>
                    <span className="radarr-setup-list__state" data-state={instance.state}><strong>{humanize(instance.state ?? "unknown")}</strong><span>{instance.reason ?? (instance.lastTestedAt ? `Tested ${timestampLabel(instance.lastTestedAt)}` : instance.state ? "Connection verified on save" : "Connection state unavailable")}</span></span>
                    <span className="radarr-setup-list__actions"><button type="button" className="btn btn--ghost btn--sm" aria-label={`Edit ${instance.name}`} onClick={() => setDialog({ kind: "instance", value: instance })}>Edit</button><button type="button" className="btn btn--ghost btn--sm" aria-label={`Archive ${instance.name}`} onClick={() => setDialog({ kind: "archive-instance", value: instance })}>Archive</button></span>
                  </li>
                ))}
              </ul>
            ) : <p className="radarr-empty">No Radarr instances are configured.</p>}
          </section>

          <section className="radarr-section" aria-labelledby="radarr-presets-title">
            <div className="radarr-section__head radarr-section__head--controls">
              <div><h3 id="radarr-presets-title">Acquisition presets</h3><p>Bundle one verified instance, target settings, and acquisition mode.</p></div>
              <button type="button" className="btn btn--ghost btn--sm" disabled={activeInstances.length === 0} onClick={() => setDialog({ kind: "preset" })}><PlusIcon aria-hidden="true" />Add preset</button>
            </div>
            {activePresets.length > 0 ? (
              <ul className="radarr-setup-list" aria-label="Radarr acquisition presets">
                {activePresets.map((preset) => (
                  <li key={preset.id}>
                    <span className="radarr-setup-list__identity"><strong>{preset.name}</strong><span>{preset.instanceName ?? activeInstances.find((instance) => String(instance.id) === String(preset.instanceId))?.name ?? "Unknown instance"}</span></span>
                    <span className="radarr-setup-list__state" data-state={preset.valid === false ? "error" : "connected"}><strong>{preset.valid === false ? "Invalid" : humanize(preset.mode)}</strong><span>{preset.invalidReason ?? `${preset.qualityProfileName ?? `Profile ${preset.qualityProfileId}`} · ${preset.rootFolderPath}`}</span></span>
                    <span className="radarr-setup-list__actions"><button type="button" className="btn btn--ghost btn--sm" aria-label={`Edit ${preset.name}`} onClick={() => setDialog({ kind: "preset", value: preset })}>Edit</button><button type="button" className="btn btn--ghost btn--sm" aria-label={`Archive ${preset.name}`} onClick={() => setDialog({ kind: "archive-preset", value: preset })}>Archive</button></span>
                  </li>
                ))}
              </ul>
            ) : <p className="radarr-empty">{activeInstances.length > 0 ? "No acquisition presets are configured." : "Add a verified instance before creating a preset."}</p>}
          </section>

          {archivedInstances.length + archivedPresets.length > 0 ? (
            <details className="radarr-archived">
              <summary>Archived setup · {archivedInstances.length + archivedPresets.length}</summary>
              <ul>{archivedInstances.map((item) => <li key={`instance-${item.id}`}><strong>{item.name}</strong><span>Instance</span></li>)}{archivedPresets.map((item) => <li key={`preset-${item.id}`}><strong>{item.name}</strong><span>Preset</span></li>)}</ul>
            </details>
          ) : null}
        </>
      )}

      {dialog?.kind === "instance" ? <RadarrInstanceDialog instance={dialog.value} onClose={() => setDialog(null)} onSaved={reload} /> : null}
      {dialog?.kind === "preset" ? <RadarrPresetDialog preset={dialog.value} instances={activeInstances} onClose={() => setDialog(null)} onSaved={reload} /> : null}
      {dialog?.kind === "archive-instance" || dialog?.kind === "archive-preset" ? <ArchiveSetupDialog dialog={dialog} onClose={() => setDialog(null)} onDone={reload} /> : null}
    </section>
  );
}
