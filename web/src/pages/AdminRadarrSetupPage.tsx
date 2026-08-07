import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ArchiveIcon, PencilIcon, PlusIcon } from "lucide-react";
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

import { humanize } from "@/components/moviepickarr/admin/radarr";
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
    <section className="radarr-page radarr-page--setup" aria-label="Radarr setup">
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
          <div className="radarr-page__toolbar" aria-label="Setup actions">
            <p>Presets inherit their connection from the instance above them.</p>
            <div>
              <button type="button" className="btn btn--ghost btn--sm" onClick={() => setDialog({ kind: "instance" })}><PlusIcon aria-hidden="true" />Add instance</button>
              <button type="button" className="btn btn--ghost btn--sm" disabled={activeInstances.length === 0} onClick={() => setDialog({ kind: "preset" })}><PlusIcon aria-hidden="true" />Add preset</button>
            </div>
          </div>

          {activeInstances.length > 0 ? (
            <div className="radarr-setup-tree" aria-label="Radarr instances and acquisition presets">
              {activeInstances.map((instance) => {
                const instancePresets = activePresets.filter(
                  (preset) => String(preset.instanceId) === String(instance.id),
                );
                return (
                  <section key={instance.id} className="radarr-setup-tree__group" aria-labelledby={`radarr-instance-${instance.id}`}>
                    <div className="radarr-setup-tree__instance">
                      <span className="radarr-setup-list__identity">
                        <strong id={`radarr-instance-${instance.id}`}>{instance.name}</strong>
                        <span>{instance.url ?? "URL stored securely"}</span>
                      </span>
                      <span className="radarr-setup-list__state" data-state={instance.state}>
                        <strong>{humanize(instance.state ?? "unknown")}</strong>
                        <span>{instance.reason ?? (instance.state ? "Connection verified on save" : "Connection state unavailable")}</span>
                      </span>
                      <span className="radarr-setup-list__actions">
                        <button type="button" className="iconbtn" title={`Edit ${instance.name}`} aria-label={`Edit ${instance.name}`} onClick={() => setDialog({ kind: "instance", value: instance })}><PencilIcon aria-hidden="true" /></button>
                        <button type="button" className="iconbtn iconbtn--danger" title={`Archive ${instance.name}`} aria-label={`Archive ${instance.name}`} onClick={() => setDialog({ kind: "archive-instance", value: instance })}><ArchiveIcon aria-hidden="true" /></button>
                      </span>
                    </div>
                    <ul className="radarr-setup-tree__presets" aria-label={`${instance.name} presets`}>
                      {instancePresets.map((preset) => (
                        <li key={preset.id} className="radarr-setup-tree__preset">
                          <span className="radarr-setup-list__identity"><strong>{preset.name}</strong><span>{humanize(preset.mode)}</span></span>
                          <span className="radarr-setup-list__state" data-state={preset.valid === false ? "error" : "connected"}>
                            <strong>{preset.valid === false ? "Invalid" : preset.qualityProfileName ?? `Profile ${preset.qualityProfileId}`}</strong>
                            <span>{preset.invalidReason ?? preset.rootFolderPath}</span>
                          </span>
                          <span className="radarr-setup-list__actions">
                            <button type="button" className="iconbtn" title={`Edit ${preset.name}`} aria-label={`Edit ${preset.name}`} onClick={() => setDialog({ kind: "preset", value: preset })}><PencilIcon aria-hidden="true" /></button>
                            <button type="button" className="iconbtn iconbtn--danger" title={`Archive ${preset.name}`} aria-label={`Archive ${preset.name}`} onClick={() => setDialog({ kind: "archive-preset", value: preset })}><ArchiveIcon aria-hidden="true" /></button>
                          </span>
                        </li>
                      ))}
                      {instancePresets.length === 0 ? <li className="radarr-setup-tree__empty">No presets</li> : null}
                    </ul>
                  </section>
                );
              })}
            </div>
          ) : <p className="radarr-empty">No Radarr instances are configured.</p>}

          {activePresets.some((preset) => !activeInstances.some((instance) => String(instance.id) === String(preset.instanceId))) ? (
            <section className="radarr-setup-tree__group radarr-setup-tree__group--orphaned" aria-labelledby="radarr-unlinked-presets">
              <div className="radarr-setup-tree__instance"><span className="radarr-setup-list__identity"><strong id="radarr-unlinked-presets">Unlinked presets</strong><span>The original instance is unavailable.</span></span></div>
              <ul className="radarr-setup-tree__presets">
                {activePresets.filter((preset) => !activeInstances.some((instance) => String(instance.id) === String(preset.instanceId))).map((preset) => (
                  <li key={preset.id} className="radarr-setup-tree__preset">
                    <span className="radarr-setup-list__identity"><strong>{preset.name}</strong><span>{humanize(preset.mode)}</span></span>
                    <span className="radarr-setup-list__state" data-state="error"><strong>Invalid</strong><span>{preset.invalidReason ?? "Instance unavailable"}</span></span>
                    <span className="radarr-setup-list__actions">
                      <button type="button" className="iconbtn" title={`Edit ${preset.name}`} aria-label={`Edit ${preset.name}`} onClick={() => setDialog({ kind: "preset", value: preset })}><PencilIcon aria-hidden="true" /></button>
                      <button type="button" className="iconbtn iconbtn--danger" title={`Archive ${preset.name}`} aria-label={`Archive ${preset.name}`} onClick={() => setDialog({ kind: "archive-preset", value: preset })}><ArchiveIcon aria-hidden="true" /></button>
                    </span>
                  </li>
                ))}
              </ul>
            </section>
          ) : null}

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
