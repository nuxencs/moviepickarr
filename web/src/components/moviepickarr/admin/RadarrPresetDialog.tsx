import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useMemo, useState } from "react";

import { IntegrationProblem } from "@/api/integrations";
import {
  createRadarrPreset,
  getRadarrInstanceOptions,
  RadarrKeys,
  updateRadarrPreset,
  type RadarrAcquisitionMode,
  type RadarrInstance,
  type RadarrPreset,
} from "@/api/radarr";

import {
  isRadarrStaleRevision,
  radarrIssueMap,
} from "@/components/moviepickarr/admin/radarr";
import { Modal } from "@/components/moviepickarr/Modal";

export function RadarrPresetDialog({
  instances,
  onClose,
  onSaved,
  preset,
}: {
  instances: RadarrInstance[];
  onClose: () => void;
  onSaved: (preset: RadarrPreset) => void;
  preset?: RadarrPreset;
}) {
  const queryClient = useQueryClient();
  const [name, setName] = useState(preset?.name ?? "");
  const [instanceID, setInstanceID] = useState(String(preset?.instanceId ?? ""));
  const [rootFolderPath, setRootFolderPath] = useState(preset?.rootFolderPath ?? "");
  const [qualityProfileID, setQualityProfileID] = useState(String(preset?.qualityProfileId ?? ""));
  const [tagIDs, setTagIDs] = useState<number[]>(preset?.tagIds ?? preset?.tags?.map((tag) => tag.id) ?? []);
  const [minimumAvailability, setMinimumAvailability] = useState(preset?.minimumAvailability ?? "");
  const [mode, setMode] = useState<RadarrAcquisitionMode | null>(preset?.mode ?? null);
  const options = useQuery({
    queryKey: RadarrKeys.instanceOptions(instanceID),
    queryFn: ({ signal }) => getRadarrInstanceOptions(instanceID, signal),
    enabled: Boolean(instanceID),
    retry: false,
    staleTime: 0,
  });
  useEffect(() => {
    if (!preset || String(preset.instanceId) !== instanceID) {
      setRootFolderPath("");
      setQualityProfileID("");
      setTagIDs([]);
    }
  }, [instanceID, preset]);
  const save = useMutation({
    mutationFn: () => {
      if (!mode) throw new Error("Acquisition mode is required");
      const draft = {
        name: name.trim(),
        instanceId: Number(instanceID),
        rootFolderPath,
        qualityProfileId: Number(qualityProfileID),
        tagIds: tagIDs,
        minimumAvailability,
        mode,
        revision: preset?.revision,
      };
      return preset ? updateRadarrPreset(preset.id, draft) : createRadarrPreset(draft);
    },
    onSuccess: onSaved,
  });
  const issues = useMemo(() => radarrIssueMap(save.error), [save.error]);
  const stale = isRadarrStaleRevision(save.error);
  const feedback = save.isError
    ? stale
      ? "Another Admin changed this preset. Reload it before saving again."
      : save.error instanceof IntegrationProblem
      ? save.error.message
      : "The acquisition preset could not be saved."
      : options.isError
      ? options.error instanceof IntegrationProblem
        ? options.error.message
        : "The selected Radarr instance could not be verified."
      : "";
  const rootExists = Boolean(options.data?.rootFolders.some(
    (root) => root.path === rootFolderPath && root.accessible !== false,
  ));
  const profileExists = Boolean(options.data?.qualityProfiles.some(
    (profile) => String(profile.id) === qualityProfileID,
  ));
  const missingTagIDs = tagIDs.filter(
    (id) => !options.data?.tags.some((tag) => tag.id === id),
  );
  const valid = Boolean(
    name.trim() &&
    instanceID &&
    rootFolderPath &&
    rootExists &&
    qualityProfileID &&
    profileExists &&
    missingTagIDs.length === 0 &&
    minimumAvailability &&
    mode &&
    options.data,
  );
  const title = preset ? "Edit acquisition preset" : "Add acquisition preset";

  return (
    <Modal label={title} className="modal--radarr-preset" capped dismissible={!save.isPending} onClose={onClose}>
      {(close) => (
        <form
          className="radarr-modal-form"
          noValidate
          onSubmit={(event) => {
            event.preventDefault();
            if (valid) save.mutate();
          }}
        >
          <header className="radarr-modal__head">
            <div><h3>{title}</h3><p>Every target value is loaded and validated against the selected instance.</p></div>
          </header>
          <div className="modal__scroll radarr-modal__scroll radarr-form">
            <label className="fieldgroup">
              <span>Name</span>
              <span className="field" data-invalid={issues.name ? true : undefined}><input autoFocus value={name} placeholder="1080p movies" aria-invalid={issues.name ? true : undefined} aria-describedby={issues.name ? "radarr-preset-name-error" : undefined} onChange={(event) => setName(event.target.value)} /></span>
              {issues.name ? <span id="radarr-preset-name-error" className="field-error">{issues.name}</span> : null}
            </label>
            <label className="fieldgroup">
              <span>Radarr instance</span>
              <span className="field" data-invalid={issues.instanceId ? true : undefined}><select value={instanceID} aria-invalid={issues.instanceId ? true : undefined} aria-describedby={issues.instanceId ? "radarr-preset-instance-error" : undefined} onChange={(event) => setInstanceID(event.target.value)}><option value="">Choose an instance</option>{instances.filter((item) => !item.archivedAt).map((item) => <option key={item.id} value={String(item.id)}>{item.name}</option>)}</select></span>
              {issues.instanceId ? <span id="radarr-preset-instance-error" className="field-error">{issues.instanceId}</span> : null}
            </label>
            {instanceID && options.isPending ? <p className="radarr-form__loading" role="status">Verifying instance options…</p> : null}
            {options.data ? (
              <>
                <label className="fieldgroup">
                  <span>Root folder</span>
                  <span className="field" data-invalid={issues.rootFolderPath || (rootFolderPath && !rootExists) ? true : undefined}><select value={rootFolderPath} aria-invalid={issues.rootFolderPath || (rootFolderPath && !rootExists) ? true : undefined} aria-describedby={issues.rootFolderPath || (rootFolderPath && !rootExists) ? "radarr-preset-root-error" : undefined} onChange={(event) => setRootFolderPath(event.target.value)}><option value="">Choose a root folder</option>{options.data.rootFolders.filter((root) => root.accessible !== false).map((root) => <option key={root.id} value={root.path}>{root.path}</option>)}</select></span>
                  {issues.rootFolderPath || (rootFolderPath && !rootExists) ? <span id="radarr-preset-root-error" className="field-error">{issues.rootFolderPath ?? "This root folder is not available on the instance."}</span> : null}
                </label>
                <label className="fieldgroup">
                  <span>Quality profile</span>
                  <span className="field" data-invalid={issues.qualityProfileId || (qualityProfileID && !profileExists) ? true : undefined}><select value={qualityProfileID} aria-invalid={issues.qualityProfileId || (qualityProfileID && !profileExists) ? true : undefined} aria-describedby={issues.qualityProfileId || (qualityProfileID && !profileExists) ? "radarr-preset-profile-error" : undefined} onChange={(event) => setQualityProfileID(event.target.value)}><option value="">Choose a quality profile</option>{options.data.qualityProfiles.map((profile) => <option key={profile.id} value={String(profile.id)}>{profile.name}</option>)}</select></span>
                  {issues.qualityProfileId || (qualityProfileID && !profileExists) ? <span id="radarr-preset-profile-error" className="field-error">{issues.qualityProfileId ?? "This quality profile is not available on the instance."}</span> : null}
                </label>
                <fieldset className="radarr-tag-fieldset" aria-invalid={issues.tagIds ? true : undefined} aria-describedby={issues.tagIds ? "radarr-preset-tags-error" : undefined}>
                  <legend>Tags <span>Optional</span></legend>
                  {options.data.tags.length > 0 ? (
                    <div className="radarr-tag-options">
                      {options.data.tags.map((tag) => (
                        <label key={tag.id} className="int-toggle">
                          <input
                            type="checkbox"
                            checked={tagIDs.includes(tag.id)}
                            onChange={(event) => setTagIDs((current) => event.target.checked ? [...current, tag.id] : current.filter((id) => id !== tag.id))}
                          />
                          <span>{tag.label ?? tag.name ?? tag.id}</span>
                        </label>
                      ))}
                    </div>
                  ) : <p>No tags are configured in this instance.</p>}
                  {missingTagIDs.length > 0 ? (
                    <p className="field-error">
                      {missingTagIDs.length} saved tag{missingTagIDs.length === 1 ? " is" : "s are"} no longer available.{" "}
                      <button type="button" className="radarr-inline-action" onClick={() => setTagIDs((current) => current.filter((id) => !missingTagIDs.includes(id)))}>
                        Remove {missingTagIDs.length === 1 ? "it" : "them"}
                      </button>
                    </p>
                  ) : null}
                  {issues.tagIds ? <p id="radarr-preset-tags-error" className="field-error">{issues.tagIds}</p> : null}
                </fieldset>
                <label className="fieldgroup">
                  <span>Minimum availability</span>
                  <span className="field" data-invalid={issues.minimumAvailability ? true : undefined}><select value={minimumAvailability} aria-invalid={issues.minimumAvailability ? true : undefined} aria-describedby={issues.minimumAvailability ? "radarr-preset-availability-error" : undefined} onChange={(event) => setMinimumAvailability(event.target.value)}><option value="">Choose availability</option><option value="tba">TBA</option><option value="announced">Announced</option><option value="inCinemas">In cinemas</option><option value="released">Released</option></select></span>
                  {issues.minimumAvailability ? <span id="radarr-preset-availability-error" className="field-error">{issues.minimumAvailability}</span> : null}
                </label>
                <fieldset className="radarr-mode-fieldset" aria-invalid={issues.mode ? true : undefined} aria-describedby={issues.mode ? "radarr-preset-mode-error" : undefined}>
                  <legend>Acquisition mode</legend>
                  <label><input type="radio" name="radarr-mode" value="manual" checked={mode === "manual"} onChange={() => setMode("manual")} /><span><strong>Manual</strong><small>Add unmonitored, then let an Admin choose the release.</small></span></label>
                  <label><input type="radio" name="radarr-mode" value="automatic" checked={mode === "automatic"} onChange={() => setMode("automatic")} /><span><strong>Automatic</strong><small>Add monitored and ask Radarr to search immediately.</small></span></label>
                  {issues.mode ? <span id="radarr-preset-mode-error" className="field-error">{issues.mode}</span> : null}
                </fieldset>
              </>
            ) : null}
            {feedback ? (
              <div className="radarr-feedback" role="alert">
                <span>{feedback}</span>
                {stale ? (
                  <button
                    type="button"
                    className="radarr-inline-action"
                    onClick={() => {
                      void queryClient.invalidateQueries({ queryKey: RadarrKeys.presets() });
                      onClose();
                    }}
                  >
                    Reload latest
                  </button>
                ) : null}
              </div>
            ) : null}
          </div>
          <footer className="radarr-modal__foot">
            <button type="button" className="btn btn--ghost" disabled={save.isPending} onClick={close}>Cancel</button>
            <button type="submit" className="btn btn--accent" disabled={!valid || save.isPending}>{save.isPending ? "Saving…" : "Save preset"}</button>
          </footer>
        </form>
      )}
    </Modal>
  );
}
