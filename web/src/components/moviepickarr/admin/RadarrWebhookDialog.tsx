import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Loader2Icon } from "lucide-react";
import { useMemo, useState } from "react";

import { IntegrationProblem } from "@/api/integrations";
import {
  createRadarrWebhook,
  RADARR_ACTION_REASONS,
  RadarrKeys,
  testRadarrWebhook,
  testRadarrWebhookDraft,
  updateRadarrWebhook,
  type RadarrWebhook,
  type RadarrWebhookDraft,
} from "@/api/radarr";

import { humanize, isRadarrStaleRevision, radarrIssueMap } from "@/components/moviepickarr/admin/radarr";
import { Modal } from "@/components/moviepickarr/Modal";
import { toast } from "@/components/ui/toast-api";

export function RadarrWebhookDialog({
  destination,
  onClose,
  onSaved,
}: {
  destination?: RadarrWebhook;
  onClose: () => void;
  onSaved: (destination: RadarrWebhook) => void;
}) {
  const queryClient = useQueryClient();
  const [name, setName] = useState(destination?.name ?? "");
  const [format, setFormat] = useState<"generic" | "discord">(destination?.format ?? "discord");
  const [url, setURL] = useState("");
  const [reasons, setReasons] = useState<string[]>(destination?.reasons ?? [...RADARR_ACTION_REASONS]);
  const [roleMention, setRoleMention] = useState(destination?.roleMention ?? "");
  const [verified, setVerified] = useState(destination?.verified ?? false);
  const hasPersistedEndpoint = Boolean(destination && !url.trim());
  const normalizedRoleMention = format === "discord" ? roleMention.trim() : "";
  const payloadChanged = Boolean(destination && (
    format !== destination.format ||
    normalizedRoleMention !== (destination.roleMention ?? "")
  ));
  const effectiveVerified = verified && !payloadChanged;
  const testNeedsReplacementURL = Boolean(destination && payloadChanged && !url.trim());
  const draft = (): RadarrWebhookDraft => ({
    name: name.trim(),
    format,
    url: url.trim() || undefined,
    enabled: Boolean(destination?.enabled && hasPersistedEndpoint && effectiveVerified),
    reasons,
    roleMention: normalizedRoleMention || undefined,
    revision: destination?.revision,
  });
  const test = useMutation<RadarrWebhook | { verified?: boolean; reason?: string }, unknown, void>({
    mutationFn: () => destination && !url.trim()
      ? testRadarrWebhook(destination.id)
      : testRadarrWebhookDraft({ ...draft(), enabled: false }),
    onSuccess: (result) => {
      const passed = result.verified !== false;
      setVerified(hasPersistedEndpoint && passed);
      const message = passed
        ? hasPersistedEndpoint
          ? "Test delivered successfully. You can enable it from the destination list."
          : "Draft test delivered successfully. Save it disabled, then test the saved destination to enable it."
        : "reason" in result && result.reason
          ? result.reason
          : "The test delivery failed.";
      if (passed) toast.success(message);
      else toast.error(message);
      if (hasPersistedEndpoint) {
        void queryClient.invalidateQueries({ queryKey: RadarrKeys.webhooks() });
      }
    },
    onError: (error) => {
      setVerified(false);
      toast.error(error instanceof IntegrationProblem ? error.message : "The test delivery failed.");
    },
  });
  const save = useMutation({
    mutationFn: () => destination
      ? updateRadarrWebhook(destination.id, draft())
      : createRadarrWebhook(draft()),
    onSuccess: onSaved,
  });
  const formError = save.error ?? test.error;
  const issues = useMemo(() => radarrIssueMap(formError), [formError]);
  const stale = isRadarrStaleRevision(save.error);
  const feedback = save.isError
    ? stale
      ? "Another Admin changed this destination. Reload it before saving again."
      : save.error instanceof IntegrationProblem
      ? save.error.message
      : "The webhook destination could not be saved."
    : "";
  const needsURL = !destination || Boolean(url.trim());
  const valid = Boolean(name.trim() && reasons.length > 0 && (!needsURL || url.trim()));
  const busy = test.isPending || save.isPending;
  const title = destination ? "Edit webhook destination" : "Add webhook destination";
  const resetVerification = () => {
    setVerified(false);
  };

  return (
    <Modal label={title} className="modal--radarr-webhook" capped dismissible={!busy} onClose={onClose}>
      {(close) => (
        <form
          className="radarr-modal-form"
          noValidate
          onSubmit={(event) => {
            event.preventDefault();
            if (valid) save.mutate();
          }}
        >
          <header className="radarr-modal__head"><div><h3>{title}</h3><p>Only actionable acquisition conditions are sent. Download completion stays in Radarr.</p></div></header>
          <div className="modal__scroll radarr-modal__scroll radarr-form">
            <label className="fieldgroup">
              <span>Name</span>
              <span className="field" data-invalid={issues.name ? true : undefined}><input autoFocus value={name} placeholder="Movie night Discord" aria-invalid={issues.name ? true : undefined} aria-describedby={issues.name ? "radarr-webhook-name-error" : undefined} onChange={(event) => setName(event.target.value)} /></span>
              {issues.name ? <span id="radarr-webhook-name-error" className="field-error">{issues.name}</span> : null}
            </label>
            <label className="fieldgroup">
              <span>Format</span>
              <span className="field" data-invalid={issues.format ? true : undefined}><select value={format} aria-invalid={issues.format ? true : undefined} aria-describedby={issues.format ? "radarr-webhook-format-error" : undefined} onChange={(event) => setFormat(event.target.value as "generic" | "discord")}><option value="discord">Discord</option><option value="generic">Generic JSON</option></select></span>
              {issues.format ? <span id="radarr-webhook-format-error" className="field-error">{issues.format}</span> : null}
            </label>
            <label className="fieldgroup">
              <span>Webhook URL</span>
              <span className="field" data-invalid={issues.url ? true : undefined}>
                <input
                  type="password"
                  autoComplete="new-password"
                  value={url}
                  placeholder={destination ? "Enter a replacement URL" : "Enter webhook URL"}
                  aria-invalid={issues.url ? true : undefined}
                  aria-describedby={issues.url ? "radarr-webhook-url-error" : undefined}
                  onChange={(event) => { setURL(event.target.value); resetVerification(); }}
                />
              </span>
              {destination && !url ? <small>{testNeedsReplacementURL ? "Enter a replacement URL to test these changes before saving." : "The saved URL is write-only and remains unchanged."}</small> : null}
              {issues.url ? <span id="radarr-webhook-url-error" className="field-error">{issues.url}</span> : null}
            </label>
            {format === "discord" ? (
              <label className="fieldgroup">
                <span>Role mention <small>Optional</small></span>
                <span className="field" data-invalid={issues.roleMention ? true : undefined}><input value={roleMention} placeholder="1234567890" aria-invalid={issues.roleMention ? true : undefined} aria-describedby={issues.roleMention ? "radarr-webhook-role-error" : undefined} onChange={(event) => setRoleMention(event.target.value)} /></span>
                <small>Enter the Discord role ID. Moviepickarr formats the mention safely.</small>
                {issues.roleMention ? <span id="radarr-webhook-role-error" className="field-error">{issues.roleMention}</span> : null}
              </label>
            ) : null}
            <fieldset className="radarr-reason-fieldset" aria-invalid={issues.reasons ? true : undefined} aria-describedby={issues.reasons ? "radarr-webhook-reasons-error" : undefined}>
              <legend>Notify for</legend>
              <div className="radarr-reason-options">
                {RADARR_ACTION_REASONS.map((reason) => (
                  <label key={reason} className="int-toggle">
                    <input
                      type="checkbox"
                      checked={reasons.includes(reason)}
                      onChange={(event) => setReasons((current) => event.target.checked ? [...current, reason] : current.filter((value) => value !== reason))}
                    />
                    <span>{humanize(reason)}</span>
                  </label>
                ))}
              </div>
              {issues.reasons || reasons.length === 0 ? <span id="radarr-webhook-reasons-error" className="field-error">{issues.reasons ?? "Choose at least one actionable reason."}</span> : null}
            </fieldset>
            {feedback ? (
              <div className="radarr-feedback" role="alert">
                <span>{feedback}</span>
                {stale ? (
                  <button
                    type="button"
                    className="radarr-inline-action"
                    onClick={() => {
                      void queryClient.invalidateQueries({ queryKey: RadarrKeys.webhooks() });
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
            <button type="button" className="btn btn--ghost" disabled={busy} onClick={close}>Cancel</button>
            <button type="button" className="btn btn--ghost" disabled={busy || !valid || testNeedsReplacementURL} onClick={() => test.mutate()}>
              {test.isPending ? <Loader2Icon className="animate-spin mg-spin" aria-hidden="true" /> : null}
              {test.isPending ? "Testing…" : "Test"}
            </button>
            <button type="submit" className="btn btn--accent" disabled={!valid || busy}>
              {save.isPending ? <Loader2Icon className="animate-spin mg-spin" aria-hidden="true" /> : null}
              {save.isPending ? "Saving…" : "Save"}
            </button>
          </footer>
        </form>
      )}
    </Modal>
  );
}
