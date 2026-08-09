import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useMemo, useState } from "react";

import { IntegrationProblem } from "@/api/integrations";
import {
  createRadarrInstance,
  RadarrKeys,
  updateRadarrInstance,
  type RadarrInstance,
} from "@/api/radarr";

import { isRadarrStaleRevision, radarrIssueMap } from "@/components/moviepickarr/admin/radarr";
import { Modal } from "@/components/moviepickarr/Modal";

function instanceAuthority(value: string) {
  try {
    const parsed = new URL(value);
    if (parsed.protocol !== "http:" && parsed.protocol !== "https:") return null;
    return `${parsed.protocol.toLowerCase()}//${parsed.host.toLowerCase()}`;
  } catch {
    return null;
  }
}

export function RadarrInstanceDialog({
  instance,
  onClose,
  onSaved,
}: {
  instance?: RadarrInstance;
  onClose: () => void;
  onSaved: (instance: RadarrInstance) => void;
}) {
  const queryClient = useQueryClient();
  const [name, setName] = useState(instance?.name ?? "");
  const [url, setURL] = useState(instance?.url ?? "");
  const [apiKey, setAPIKey] = useState("");
  const save = useMutation({
    mutationFn: () => {
      const draft = {
        name: name.trim(),
        url: url.trim(),
        apiKey: apiKey.trim() || undefined,
        revision: instance?.revision,
      };
      return instance ? updateRadarrInstance(instance.id, draft) : createRadarrInstance(draft);
    },
    onSuccess: onSaved,
  });
  const issues = useMemo(() => radarrIssueMap(save.error), [save.error]);
  const stale = isRadarrStaleRevision(save.error);
  const feedback = save.isError
    ? stale
      ? "Another Admin changed this instance. Reload it before saving again."
      : save.error instanceof IntegrationProblem
      ? save.error.message
      : "The Radarr instance could not be saved."
    : "";
  const currentAuthority = instanceAuthority(url.trim());
  const authorityChanged = Boolean(
    instance && currentAuthority && currentAuthority !== instanceAuthority(instance.url ?? ""),
  );
  const apiKeyRequired = !instance?.apiKeyConfigured || authorityChanged;
  const valid = Boolean(name.trim() && url.trim() && (!apiKeyRequired || apiKey.trim()));
  const title = instance ? "Edit Radarr instance" : "Add Radarr instance";

  return (
    <Modal label={title} className="modal--form" dismissible={!save.isPending} onClose={onClose}>
      {(close) => (
        <form
          className="adm-sheet radarr-form"
          noValidate
          onSubmit={(event) => {
            event.preventDefault();
            if (valid) save.mutate();
          }}
        >
          <h3 className="adm-modal__title">{title}</h3>
          <p className="adm-modal__sub">The live connection and credentials are tested before saving.</p>
          <label className="fieldgroup">
            <span>Name</span>
            <span className="field" data-invalid={issues.name ? true : undefined}>
              <input
                autoFocus
                value={name}
                placeholder="1080p movies"
                aria-invalid={issues.name ? true : undefined}
                aria-describedby={issues.name ? "radarr-instance-name-error" : undefined}
                onChange={(event) => setName(event.target.value)}
              />
            </span>
            {issues.name ? <span id="radarr-instance-name-error" className="field-error">{issues.name}</span> : null}
          </label>
          <label className="fieldgroup">
            <span>Radarr URL</span>
            <span className="field" data-invalid={issues.url ? true : undefined}>
              <input
                type="url"
                value={url}
                placeholder="https://radarr.example.test"
                aria-invalid={issues.url ? true : undefined}
                aria-describedby={issues.url ? "radarr-instance-url-error" : undefined}
                onChange={(event) => setURL(event.target.value)}
              />
            </span>
            {issues.url ? <span id="radarr-instance-url-error" className="field-error">{issues.url}</span> : null}
          </label>
          <label className="fieldgroup">
            <span>API key</span>
            <span className="field" data-invalid={issues.apiKey ? true : undefined}>
              <input
                type="password"
                autoComplete="new-password"
                value={apiKey}
                placeholder={instance?.apiKeyConfigured ? "Enter a replacement key" : "Enter API key"}
                aria-invalid={issues.apiKey ? true : undefined}
                aria-describedby={issues.apiKey ? "radarr-instance-key-error" : undefined}
                onChange={(event) => setAPIKey(event.target.value)}
              />
            </span>
            {authorityChanged && !apiKey ? (
              <small>Enter the API key again because the URL scheme or host changed.</small>
            ) : instance?.apiKeyConfigured && !apiKey ? (
              <small>The saved key will remain unchanged.</small>
            ) : null}
            {issues.apiKey ? <span id="radarr-instance-key-error" className="field-error">{issues.apiKey}</span> : null}
          </label>
          {feedback ? (
            <div className="radarr-feedback" role="alert">
              <span>{feedback}</span>
              {stale ? (
                <button
                  type="button"
                  className="radarr-inline-action"
                  onClick={() => {
                    void queryClient.invalidateQueries({ queryKey: RadarrKeys.instances() });
                    onClose();
                  }}
                >
                  Reload latest
                </button>
              ) : null}
            </div>
          ) : null}
          <div className="adm-modal__actions">
            <button type="button" className="btn btn--ghost" disabled={save.isPending} onClick={close}>Cancel</button>
            <button type="submit" className="btn btn--accent" disabled={!valid || save.isPending}>
              {save.isPending ? "Testing…" : "Test and save"}
            </button>
          </div>
        </form>
      )}
    </Modal>
  );
}
