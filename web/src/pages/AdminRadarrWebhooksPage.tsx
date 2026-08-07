import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { PlusIcon } from "lucide-react";
import { useState } from "react";

import { IntegrationProblem } from "@/api/integrations";
import {
  archiveRadarrWebhook,
  listRadarrWebhooks,
  RadarrKeys,
  testRadarrWebhook,
  type RadarrWebhook,
} from "@/api/radarr";

import { humanize, timestampLabel } from "@/components/moviepickarr/admin/radarr";
import { RadarrWebhookDialog } from "@/components/moviepickarr/admin/RadarrWebhookDialog";
import { Modal } from "@/components/moviepickarr/Modal";

export function AdminRadarrWebhooksPage() {
  const queryClient = useQueryClient();
  const destinations = useQuery({
    queryKey: RadarrKeys.webhooks(),
    queryFn: ({ signal }) => listRadarrWebhooks(signal),
    retry: false,
  });
  const [editing, setEditing] = useState<RadarrWebhook | "new" | null>(null);
  const [archiving, setArchiving] = useState<RadarrWebhook | null>(null);
  const [feedback, setFeedback] = useState("");
  const test = useMutation({
    mutationFn: testRadarrWebhook,
    onSuccess: () => {
      setFeedback("Test delivered successfully.");
      void queryClient.invalidateQueries({ queryKey: RadarrKeys.webhooks() });
    },
    onError: (error) => setFeedback(error instanceof IntegrationProblem ? error.message : "The test delivery failed."),
  });
  const archive = useMutation({
    mutationFn: archiveRadarrWebhook,
    onSuccess: () => {
      setArchiving(null);
      void queryClient.invalidateQueries({ queryKey: RadarrKeys.webhooks() });
    },
  });
  const active = destinations.data?.filter((item) => !item.archivedAt) ?? [];
  const archived = destinations.data?.filter((item) => item.archivedAt) ?? [];
  const archiveFeedback = archive.isError
    ? archive.error instanceof IntegrationProblem ? archive.error.message : "The destination could not be archived."
    : "";

  return (
    <section className="radarr-page" aria-labelledby="radarr-webhooks-title">
      <div className="sec-head radarr-page__head"><div className="sec-title"><h2 id="radarr-webhooks-title">Webhooks</h2></div></div>
      {destinations.isPending ? (
        <div className="adm-state" role="status">Loading webhook destinations…</div>
      ) : destinations.isError ? (
        <div className="adm-state" role="alert">
          {destinations.error instanceof IntegrationProblem && destinations.error.status === 403
            ? "Admin access is required to manage Radarr webhooks."
            : "Webhook destinations could not be loaded."}
        </div>
      ) : (
        <section className="radarr-section" aria-labelledby="radarr-destinations-title">
          <div className="radarr-section__head radarr-section__head--controls">
            <div><h3 id="radarr-destinations-title">Destinations</h3><p>Send Discord embeds or generic JSON only when an Admin can act in Moviepickarr.</p></div>
            <button type="button" className="btn btn--ghost btn--sm" onClick={() => setEditing("new")}><PlusIcon aria-hidden="true" />Add destination</button>
          </div>
          {feedback ? <p className="radarr-action-feedback" role="status">{feedback}</p> : null}
          {active.length > 0 ? (
            <ul className="radarr-setup-list" aria-label="Radarr webhook destinations">
              {active.map((destination) => {
                const unhealthy = destination.health && !["healthy", "connected"].includes(destination.health);
                return (
                  <li key={destination.id}>
                    <span className="radarr-setup-list__identity"><strong>{destination.name}</strong><span>{destination.format === "discord" ? "Discord embed" : "Generic JSON"} · {destination.enabled ? "Enabled" : "Disabled"}</span></span>
                    <span className="radarr-setup-list__state" data-state={unhealthy ? "error" : destination.verified ? "connected" : "unverified"}><strong>{unhealthy ? "Delivery warning" : destination.verified ? "Verified" : "Test required"}</strong><span>{destination.healthReason ?? (destination.lastTestedAt ? `Tested ${timestampLabel(destination.lastTestedAt)}` : `${destination.reasons.length} reason filter${destination.reasons.length === 1 ? "" : "s"}`)}</span></span>
                    <span className="radarr-setup-list__actions"><button type="button" className="btn btn--ghost btn--sm" aria-label={`Test ${destination.name}`} disabled={test.isPending} onClick={() => { setFeedback(""); test.mutate(destination.id); }}>{test.isPending ? "Testing…" : "Test"}</button><button type="button" className="btn btn--ghost btn--sm" aria-label={`Edit ${destination.name}`} onClick={() => setEditing(destination)}>Edit</button><button type="button" className="btn btn--ghost btn--sm" aria-label={`Archive ${destination.name}`} onClick={() => setArchiving(destination)}>Archive</button></span>
                  </li>
                );
              })}
            </ul>
          ) : <p className="radarr-empty">No webhook destinations are configured.</p>}
          {archived.length > 0 ? <details className="radarr-archived"><summary>Archived destinations · {archived.length}</summary><ul>{archived.map((item) => <li key={item.id}><strong>{item.name}</strong><span>{humanize(item.format)}</span></li>)}</ul></details> : null}
        </section>
      )}

      {editing ? <RadarrWebhookDialog destination={editing === "new" ? undefined : editing} onClose={() => setEditing(null)} onSaved={() => { setEditing(null); void queryClient.invalidateQueries({ queryKey: RadarrKeys.webhooks() }); }} /> : null}
      {archiving ? (
        <Modal label={`Archive ${archiving.name}?`} className="modal--form" dismissible={!archive.isPending} onClose={() => setArchiving(null)}>
          {(close) => <div className="adm-sheet"><h3 className="adm-modal__title">Archive {archiving.name}?</h3><p className="adm-modal__sub">The destination stops receiving new events. Its resolved delivery diagnostics remain under the retention policy.</p>{archiveFeedback ? <p className="radarr-feedback" role="alert">{archiveFeedback}</p> : null}<div className="adm-modal__actions"><button type="button" className="btn btn--ghost" disabled={archive.isPending} onClick={close}>Keep destination</button><button type="button" className="btn btn--danger" disabled={archive.isPending} onClick={() => archive.mutate(archiving.id)}>{archive.isPending ? "Archiving…" : "Archive destination"}</button></div></div>}
        </Modal>
      ) : null}
    </section>
  );
}
