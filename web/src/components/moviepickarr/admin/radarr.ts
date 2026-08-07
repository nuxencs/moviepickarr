import { IntegrationProblem } from "@/api/integrations";
import type {
  RadarrAcquisition,
  RadarrAcquisitionStatus,
  RadarrActionReason,
  RadarrTargetSnapshot,
  RadarrTag,
} from "@/api/radarr";

export function radarrIssueMap(error: unknown): Record<string, string> {
  if (!(error instanceof IntegrationProblem)) return {};
  return Object.fromEntries(error.issues.map((issue) => [issue.field, issue.message]));
}

export function isRadarrStaleRevision(error: unknown) {
  return error instanceof IntegrationProblem && error.title === "stale_revision";
}

export const RADARR_STATUS_LABELS: Record<string, string> = {
  needs_preset: "Needs preset",
  needs_release: "Needs release",
  waiting_for_radarr: "Waiting for Radarr",
  queued: "Queued",
  downloading: "Downloading",
  importing: "Importing",
  downloaded: "Downloaded",
  action_needed: "Action needed",
  abandoned: "Abandoned",
};

export const RADARR_REASON_LABELS: Record<string, string> = {
  preset_required: "Choose an acquisition preset",
  identity_required: "Confirm the movie identity",
  release_required: "Choose a release",
  configuration_invalid: "Repair the selected configuration",
  connection_failed: "Restore the Radarr connection",
  add_failed: "Retry adding the movie",
  no_releases: "No releases were found",
  release_failed: "The selected release failed",
  import_failed: "Radarr could not import the download",
  monitoring_failed: "Radarr monitoring could not be enabled",
};

const DATE_FORMATTER = new Intl.DateTimeFormat("en-US", {
  day: "numeric",
  hour: "numeric",
  minute: "2-digit",
  month: "short",
  timeZone: "UTC",
  timeZoneName: "short",
  year: "numeric",
});

export function radarrStatusLabel(status: RadarrAcquisitionStatus) {
  return RADARR_STATUS_LABELS[status] ?? humanize(status);
}

export function radarrReasonLabel(reason?: RadarrActionReason) {
  if (!reason) return undefined;
  return RADARR_REASON_LABELS[reason] ?? humanize(reason);
}

export function humanize(value: string) {
  const spaced = value.replace(/[-_]+/g, " ").trim();
  return spaced ? spaced.charAt(0).toUpperCase() + spaced.slice(1) : "Unknown";
}

export function timestampLabel(value?: string) {
  if (!value) return "Not available";
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? "Recorded" : DATE_FORMATTER.format(date);
}

export function formatBytes(value?: number) {
  if (!value || value < 0 || !Number.isFinite(value)) return "Size unavailable";
  const units = ["B", "KB", "MB", "GB", "TB"];
  let amount = value;
  let unit = 0;
  while (amount >= 1024 && unit < units.length - 1) {
    amount /= 1024;
    unit += 1;
  }
  return `${amount >= 10 || unit === 0 ? amount.toFixed(0) : amount.toFixed(1)} ${units[unit]}`;
}

export function tagLabel(tag: RadarrTag | number | string) {
  if (typeof tag === "number" || typeof tag === "string") return String(tag);
  return tag.label ?? tag.name ?? String(tag.id);
}

export function targetName(target?: RadarrTargetSnapshot) {
  return target?.presetName ?? target?.instanceName ?? "No target selected";
}

export function acquisitionUpdatedAt(acquisition: RadarrAcquisition) {
  return acquisition.updatedAt ?? acquisition.milestones?.updatedAt ?? acquisition.createdAt;
}

export function acquisitionIsOpen(acquisition: RadarrAcquisition) {
  return acquisition.status !== "downloaded" && acquisition.status !== "abandoned";
}

export function acquisitionTitle(acquisition: RadarrAcquisition) {
  return acquisition.title?.trim() || acquisition.identity?.title?.trim() || "Untitled movie";
}

export function acquisitionPreviewReady(acquisition: RadarrAcquisition) {
  return acquisition.previewReady ?? Boolean(acquisition.targetPreviewedAt);
}

export function acquisitionUsesExisting(acquisition: RadarrAcquisition) {
  return acquisition.targetPreviewExisting ??
    acquisition.existingMovie ??
    acquisition.existing ??
    acquisition.adoptedExisting ??
    false;
}
