import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ArchiveIcon, Loader2Icon, PencilIcon, PlusIcon, SendIcon } from "lucide-react";
import { useState } from "react";

import { IntegrationProblem } from "@/api/integrations";
import {
  archiveRadarrWebhook,
  listRadarrWebhooks,
  RadarrKeys,
  testRadarrWebhook,
  updateRadarrWebhook,
  type RadarrWebhook,
} from "@/api/radarr";

import { humanize } from "@/components/moviepickarr/admin/radarr";
import { RadarrDisclosure } from "@/components/moviepickarr/admin/RadarrDisclosure";
import { RadarrWebhookDialog } from "@/components/moviepickarr/admin/RadarrWebhookDialog";
import { Modal } from "@/components/moviepickarr/Modal";
import { toast } from "@/components/ui/toast-api";

export function AdminRadarrWebhooksPage() {
  const queryClient = useQueryClient();
  const destinations = useQuery({
    queryKey: RadarrKeys.webhooks(),
    queryFn: ({ signal }) => listRadarrWebhooks(signal),
    retry: false,
  });
  const [editing, setEditing] = useState<RadarrWebhook | "new" | null>(null);
  const [archiving, setArchiving] = useState<RadarrWebhook | null>(null);
  const test = useMutation({
    mutationFn: testRadarrWebhook,
    onSuccess: (tested) => {
      toast.success(`Test delivered to ${tested.name}`);
      queryClient.setQueryData<RadarrWebhook[]>(RadarrKeys.webhooks(), (current) =>
        current?.map((item) => item.id === tested.id ? tested : item),
      );
      void queryClient.invalidateQueries({ queryKey: RadarrKeys.webhooks() });
    },
    onError: (error) => toast.error(error instanceof IntegrationProblem ? error.message : "The test delivery failed."),
  });
  const toggle = useMutation({
    mutationFn: (destination: RadarrWebhook) => updateRadarrWebhook(destination.id, {
      name: destination.name,
      format: destination.format,
      enabled: !destination.enabled,
      reasons: destination.reasons,
      roleMention: destination.roleMention,
      revision: destination.revision,
    }),
    onSuccess: (updated) => {
      toast.success(`${updated.name} ${updated.enabled ? "enabled" : "disabled"}`);
      queryClient.setQueryData<RadarrWebhook[]>(RadarrKeys.webhooks(), (current) =>
        current?.map((item) => item.id === updated.id ? updated : item),
      );
    },
    onError: (error) => toast.error(
      error instanceof IntegrationProblem ? error.message : "The destination could not be updated.",
    ),
    onSettled: () => {
      void queryClient.invalidateQueries({ queryKey: RadarrKeys.webhooks() });
    },
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
    <section className="radarr-page radarr-page--webhooks mg-rise" aria-label="Radarr webhooks">
      {destinations.isPending ? (
        <div className="adm-state" role="status">Loading webhook destinations…</div>
      ) : destinations.isError ? (
        <div className="adm-state" role="alert">
          {destinations.error instanceof IntegrationProblem && destinations.error.status === 403
            ? "Admin access is required to manage Radarr webhooks."
            : "Webhook destinations could not be loaded."}
        </div>
      ) : (
        <>
          <div className="radarr-page__toolbar">
            <p>Send Discord or generic JSON only when an Admin can act in Moviepickarr.</p>
            <div><button type="button" className="btn btn--ghost btn--sm" onClick={() => setEditing("new")}><PlusIcon aria-hidden="true" />Add destination</button></div>
          </div>
          {active.length > 0 ? (
            <ul className="radarr-setup-list radarr-setup-list--webhooks" aria-label="Radarr webhook destinations">
              {active.map((destination) => {
                const unhealthy = destination.health && !["healthy", "connected"].includes(destination.health);
                const testing = test.isPending && test.variables === destination.id;
                const toggling = toggle.isPending && toggle.variables?.id === destination.id;
                const enableBlocked = !destination.verified && !destination.enabled;
                const enableTooltipID = `radarr-webhook-enable-tooltip-${destination.id}`;
                return (
                  <li key={destination.id}>
                    <label
                      className="radarr-webhook-switch"
                      data-blocked={enableBlocked ? true : undefined}
                      data-pending={toggling ? true : undefined}
                      tabIndex={enableBlocked ? 0 : undefined}
                      aria-label={enableBlocked ? `Enable ${destination.name}` : undefined}
                      aria-disabled={enableBlocked ? true : undefined}
                      aria-describedby={enableBlocked ? enableTooltipID : undefined}
                    >
                      <span className="vis-hidden">{destination.enabled ? "Disable" : "Enable"} {destination.name}</span>
                      <input
                        type="checkbox"
                        role="switch"
                        checked={destination.enabled}
                        disabled={toggling || testing || enableBlocked}
                        aria-describedby={enableBlocked ? enableTooltipID : undefined}
                        onChange={() => toggle.mutate(destination)}
                      />
                      {toggling ? <Loader2Icon className="animate-spin mg-spin" aria-hidden="true" /> : null}
                      {enableBlocked ? <span id={enableTooltipID} className="radarr-webhook-switch__tooltip" role="tooltip">Test this destination before enabling it.</span> : null}
                    </label>
                    <span className="radarr-setup-list__identity" data-state={unhealthy ? "error" : undefined}>
                      <strong>{destination.name}</strong>
                      <span>{unhealthy
                        ? `Delivery warning: ${destination.healthReason ?? "Review the last delivery."}`
                        : `${destination.format === "discord" ? "Discord" : "Generic JSON"} · ${destination.reasons.length} reason filter${destination.reasons.length === 1 ? "" : "s"}`}</span>
                    </span>
                    <span className="radarr-setup-list__actions">
                      <button type="button" className="iconbtn" aria-label={`Test ${destination.name}`} title={`Test ${destination.name}`} aria-busy={testing} disabled={test.isPending || toggle.isPending} onClick={() => test.mutate(destination.id)}>
                        {testing ? <Loader2Icon className="animate-spin mg-spin" aria-hidden="true" /> : <SendIcon aria-hidden="true" />}
                      </button>
                      <button type="button" className="iconbtn" aria-label={`Edit ${destination.name}`} title={`Edit ${destination.name}`} disabled={toggling || testing} onClick={() => setEditing(destination)}><PencilIcon aria-hidden="true" /></button>
                      <button type="button" className="iconbtn iconbtn--danger" aria-label={`Archive ${destination.name}`} title={`Archive ${destination.name}`} disabled={toggling || testing} onClick={() => setArchiving(destination)}><ArchiveIcon aria-hidden="true" /></button>
                    </span>
                  </li>
                );
              })}
            </ul>
          ) : <p className="radarr-empty">No webhook destinations are configured.</p>}
          {archived.length > 0 ? (
            <RadarrDisclosure
              className="radarr-disclosure--archived"
              title="Archived destinations"
              meta={archived.length}
            >
              <ul>{archived.map((item) => <li key={item.id}><strong>{item.name}</strong><span>{humanize(item.format)}</span></li>)}</ul>
            </RadarrDisclosure>
          ) : null}
        </>
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
