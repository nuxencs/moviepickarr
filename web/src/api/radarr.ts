import { queryOptions } from "@tanstack/react-query";

import { integrationRequest } from "@/api/integrations";

export type RadarrID = number | string;

export type RadarrAcquisitionStatus =
  | "needs_preset"
  | "needs_release"
  | "waiting_for_radarr"
  | "queued"
  | "downloading"
  | "importing"
  | "downloaded"
  | "action_needed"
  | "abandoned"
  | "canceled"
  | string;

export type RadarrActionReason =
  | "preset_required"
  | "identity_required"
  | "release_required"
  | "configuration_invalid"
  | "connection_failed"
  | "add_failed"
  | "no_releases"
  | "release_failed"
  | "import_failed"
  | "monitoring_failed"
  | string;

export type RadarrAcquisitionMode = "manual" | "automatic";

export type RadarrAcquisitionMutationState =
  | "idle"
  | "adding"
  | "searching"
  | "grabbing"
  | "checking_replacement"
  | "recreating"
  | string;

export interface RadarrIdentity {
  tmdbId?: number;
  imdbId?: string;
  title?: string;
  year?: number;
  source?: string;
}

export interface RadarrTag {
  id: number;
  label?: string;
  name?: string;
}

export interface RadarrTargetSnapshot {
  presetId?: RadarrID;
  presetName?: string;
  instanceId?: RadarrID;
  instanceName?: string;
  rootFolderPath?: string;
  qualityProfileId?: number;
  qualityProfileName?: string;
  tags?: Array<RadarrTag | number | string>;
  minimumAvailability?: string;
  mode?: RadarrAcquisitionMode;
}

export interface RadarrEffectiveConfig {
  rootFolderPath?: string;
  qualityProfileId?: number;
  qualityProfileName?: string;
  tags?: Array<RadarrTag | number | string>;
  minimumAvailability?: string;
  monitored?: boolean;
}

export interface RadarrReleaseSummary {
  title?: string;
  quality?: string;
  selectedAt?: string;
}

export interface RadarrAcquisitionMilestones {
  createdAt?: string;
  revealedAt?: string;
  targetSelectedAt?: string;
  addedAt?: string;
  grabbedAt?: string;
  downloadedAt?: string;
  abandonedAt?: string;
  canceledAt?: string;
  updatedAt?: string;
}

export interface RadarrAcquisition {
  id: RadarrID;
  movieId?: number;
  title?: string;
  year?: number;
  status: RadarrAcquisitionStatus;
  source?: "draw" | "wildcard" | string;
  wildcardId?: number;
  actionReason?: RadarrActionReason;
  actionMessage?: string;
  mutationState: RadarrAcquisitionMutationState;
  identity?: RadarrIdentity;
  preset?: RadarrTargetSnapshot;
  target?: RadarrTargetSnapshot;
  targetLocked?: boolean;
  targetPreviewExisting?: boolean;
  targetPreviewedAt?: string;
  previewReady?: boolean;
  radarrMovieId?: number;
  existing?: boolean;
  existingMovie?: boolean;
  adoptedExisting?: boolean;
  effectiveConfig?: RadarrEffectiveConfig;
  activeQueue?: boolean;
  latestRelease?: RadarrReleaseSummary;
  manualAttemptCount?: number;
  latestFailure?: string;
  abandonmentReason?: string;
  milestones?: RadarrAcquisitionMilestones;
  createdAt?: string;
  updatedAt?: string;
}

export interface RadarrAttention {
  count: number;
}

export type RadarrAbandonmentActivity = "active" | "inactive" | "unavailable" | "not_applicable" | "complete";

export interface RadarrAbandonmentReview {
  acquisition: RadarrAcquisition;
  activity: RadarrAbandonmentActivity;
}

export interface RadarrInstance {
  id: RadarrID;
  name: string;
  url?: string;
  state?: string;
  reason?: string;
  apiKeyConfigured?: boolean;
  lastTestedAt?: string;
  archivedAt?: string;
  revision?: number;
  used?: boolean;
}

export interface RadarrRootFolder {
  id: number;
  path: string;
  freeSpace?: number;
  accessible?: boolean;
}

export interface RadarrQualityProfile {
  id: number;
  name: string;
}

export interface RadarrInstanceOptions {
  rootFolders: RadarrRootFolder[];
  qualityProfiles: RadarrQualityProfile[];
  tags: RadarrTag[];
}

export interface RadarrPreset {
  id: RadarrID;
  name: string;
  instanceId: RadarrID;
  instanceName?: string;
  rootFolderPath: string;
  qualityProfileId: number;
  qualityProfileName?: string;
  tagIds?: number[];
  tags?: RadarrTag[];
  minimumAvailability: string;
  mode: RadarrAcquisitionMode;
  valid?: boolean;
  invalidReason?: string;
  archivedAt?: string;
  revision?: number;
  used?: boolean;
}

export interface RadarrRemovalResult {
  outcome: "deleted" | "archived";
}

export const RADARR_ACTION_REASONS = [
  "preset_required",
  "identity_required",
  "release_required",
  "configuration_invalid",
  "connection_failed",
  "add_failed",
  "no_releases",
  "release_failed",
  "import_failed",
  "monitoring_failed",
] as const;

export interface RadarrWebhook {
  id: RadarrID;
  name: string;
  format: "generic" | "discord";
  enabled: boolean;
  verified: boolean;
  reasons: string[];
  roleMention?: string;
  health?: string;
  healthReason?: string;
  lastTestedAt?: string;
  archivedAt?: string;
  revision?: number;
}

export interface RadarrIdentityResult {
  tmdbId: number;
  imdbId?: string;
  title: string;
  year?: number;
  overview?: string;
}

export interface RadarrRelease {
  id: string;
  title: string;
  quality?: string;
  size?: number;
  ageHours?: number;
  peers?: number;
  protocol?: string;
  indexer?: string;
  customFormats?: string[];
  customFormatScore?: number;
  approved?: boolean;
  rejected?: boolean;
  rejections?: string[];
  mapped?: boolean;
  grabAllowed?: boolean;
}

export interface RadarrInstanceDraft {
  name: string;
  url: string;
  apiKey?: string;
  revision?: number;
}

export interface RadarrPresetDraft {
  name: string;
  instanceId: RadarrID;
  rootFolderPath: string;
  qualityProfileId: number;
  tagIds: number[];
  minimumAvailability: string;
  mode: RadarrAcquisitionMode;
  revision?: number;
}

export interface RadarrWebhookDraft {
  name: string;
  format: "generic" | "discord";
  url?: string;
  enabled: boolean;
  reasons: string[];
  roleMention?: string;
  revision?: number;
}

type ItemEnvelope<T> = T[] | { items?: T[]; nextCursor?: string };

function itemsFrom<T>(value: ItemEnvelope<T>): T[] {
  return Array.isArray(value) ? value : Array.isArray(value.items) ? value.items : [];
}

function idPath(id: RadarrID) {
  return encodeURIComponent(String(id));
}

function stringValue(value: unknown) {
  return typeof value === "string" && value.trim() ? value : undefined;
}

function numberValue(value: unknown) {
  return typeof value === "number" && Number.isFinite(value) ? value : undefined;
}

function booleanValue(value: unknown) {
  return typeof value === "boolean" ? value : undefined;
}

export const RadarrKeys = {
  all: ["integrations", "radarr"] as const,
  attention: () => [...RadarrKeys.all, "attention"] as const,
  acquisitions: () => [...RadarrKeys.all, "acquisitions"] as const,
  acquisition: (id: RadarrID) => [...RadarrKeys.acquisitions(), id] as const,
  abandonmentReview: (id: RadarrID) => [...RadarrKeys.acquisition(id), "abandonment-review"] as const,
  instances: () => [...RadarrKeys.all, "instances"] as const,
  instanceOptions: (id: RadarrID) => [...RadarrKeys.instances(), id, "options"] as const,
  presets: () => [...RadarrKeys.all, "presets"] as const,
  webhooks: () => [...RadarrKeys.all, "webhooks"] as const,
};

export const RadarrAttentionQueryOptions = (enabled = true) =>
  queryOptions({
    queryKey: RadarrKeys.attention(),
    queryFn: ({ signal }) => getRadarrAttention(signal),
    enabled,
    refetchInterval: () =>
      typeof document === "undefined" || document.visibilityState === "visible" ? 30_000 : false,
    refetchOnWindowFocus: true,
    retry: false,
    staleTime: 0,
  });

export async function getRadarrAttention(signal?: AbortSignal): Promise<RadarrAttention> {
  const value = await integrationRequest<
    number | { count?: number; attentionCount?: number; unresolved?: number }
  >("/api/v1/integrations/radarr/attention", { method: "GET", signal });
  if (typeof value === "number") return { count: Math.max(0, value) };
  const count = value.count ?? value.attentionCount ?? value.unresolved ?? 0;
  return { count: Number.isFinite(count) ? Math.max(0, count) : 0 };
}

export async function listRadarrAcquisitions(signal?: AbortSignal) {
  const value = await integrationRequest<ItemEnvelope<RadarrAcquisition>>(
    "/api/v1/integrations/radarr/acquisitions",
    { method: "GET", signal },
  );
  return itemsFrom(value);
}

export function getRadarrAcquisition(id: RadarrID, signal?: AbortSignal) {
  return integrationRequest<RadarrAcquisition>(
    `/api/v1/integrations/radarr/acquisitions/${idPath(id)}`,
    { method: "GET", signal },
  );
}

export function selectRadarrPreset(id: RadarrID, presetId: RadarrID) {
  return integrationRequest<RadarrAcquisition>(
    `/api/v1/integrations/radarr/acquisitions/${idPath(id)}/preset`,
    { method: "PUT", body: JSON.stringify({ presetId }) },
  );
}

export function confirmRadarrTarget(id: RadarrID) {
  return integrationRequest<RadarrAcquisition>(
    `/api/v1/integrations/radarr/acquisitions/${idPath(id)}/confirm`,
    { method: "POST" },
  );
}

export async function searchRadarrIdentity(id: RadarrID, query: string) {
  const value = await integrationRequest<ItemEnvelope<RadarrIdentityResult>>(
    `/api/v1/integrations/radarr/acquisitions/${idPath(id)}/identity-search`,
    { method: "POST", body: JSON.stringify({ query }) },
  );
  return itemsFrom(value);
}

export function selectRadarrIdentity(id: RadarrID, tmdbId: number) {
  return integrationRequest<RadarrAcquisition>(
    `/api/v1/integrations/radarr/acquisitions/${idPath(id)}/identity`,
    { method: "PUT", body: JSON.stringify({ tmdbId }) },
  );
}

export async function searchRadarrReleases(id: RadarrID) {
  const value = await integrationRequest<ItemEnvelope<Record<string, unknown>>>(
    `/api/v1/integrations/radarr/acquisitions/${idPath(id)}/releases/search`,
    { method: "POST" },
  );
  return itemsFrom(value).map((release, index): RadarrRelease => {
    const resultId = stringValue(release.id) ?? stringValue(release.resultId);
    const quality = typeof release.quality === "object" && release.quality !== null
      ? stringValue((release.quality as Record<string, unknown>).name)
      : stringValue(release.quality);
    const publishedAt = stringValue(release.publishedAt);
    const published = publishedAt ? new Date(publishedAt).getTime() : Number.NaN;
    const ageHours = numberValue(release.ageHours) ?? (
      Number.isNaN(published) ? undefined : Math.max(0, (Date.now() - published) / 3_600_000)
    );
    const rejectionValues = Array.isArray(release.rejections)
      ? release.rejections
      : Array.isArray(release.rejectionReasons)
        ? release.rejectionReasons
        : undefined;
    const customFormats = Array.isArray(release.customFormats)
      ? release.customFormats.filter((value): value is string => typeof value === "string")
      : undefined;
    return {
      id: resultId ?? `unavailable-${index}`,
      title: stringValue(release.title) ?? "Unnamed release",
      quality,
      size: numberValue(release.size),
      ageHours,
      peers: numberValue(release.peers) ?? numberValue(release.seeders),
      protocol: stringValue(release.protocol),
      indexer: stringValue(release.indexer),
      customFormats,
      customFormatScore: numberValue(release.customFormatScore),
      approved: booleanValue(release.approved),
      rejected: booleanValue(release.rejected),
      rejections: rejectionValues?.filter((value): value is string => typeof value === "string"),
      mapped: booleanValue(release.mapped),
      grabAllowed: Boolean(resultId) && booleanValue(release.grabAllowed) !== false,
    };
  });
}

export function grabRadarrRelease(id: RadarrID, resultId: string, override: boolean) {
  return integrationRequest<RadarrAcquisition>(
    `/api/v1/integrations/radarr/acquisitions/${idPath(id)}/releases/${idPath(resultId)}/grab`,
    { method: "POST", body: JSON.stringify({ override }) },
  );
}

export function retryRadarrAcquisition(id: RadarrID) {
  return integrationRequest<RadarrAcquisition>(
    `/api/v1/integrations/radarr/acquisitions/${idPath(id)}/retry`,
    { method: "POST" },
  );
}

export function reviewRadarrAbandonment(id: RadarrID, signal?: AbortSignal) {
  return integrationRequest<RadarrAbandonmentReview>(
    `/api/v1/integrations/radarr/acquisitions/${idPath(id)}/abandon/review`,
    { method: "POST", signal },
  );
}

export function abandonRadarrAcquisition(
  id: RadarrID,
  reason: string,
  acknowledgedActivity?: RadarrAbandonmentActivity,
) {
  return integrationRequest<RadarrAcquisition>(
    `/api/v1/integrations/radarr/acquisitions/${idPath(id)}/abandon`,
    { method: "POST", body: JSON.stringify({ reason, acknowledgedActivity }) },
  );
}

export async function listRadarrInstances(signal?: AbortSignal) {
  const value = await integrationRequest<ItemEnvelope<RadarrInstance>>(
    "/api/v1/integrations/radarr/instances",
    { method: "GET", signal },
  );
  return itemsFrom(value);
}

export function createRadarrInstance(draft: RadarrInstanceDraft) {
  return integrationRequest<RadarrInstance>("/api/v1/integrations/radarr/instances", {
    method: "POST",
    body: JSON.stringify(draft),
  });
}

export function updateRadarrInstance(id: RadarrID, draft: RadarrInstanceDraft) {
  return integrationRequest<RadarrInstance>(
    `/api/v1/integrations/radarr/instances/${idPath(id)}`,
    { method: "PUT", body: JSON.stringify(draft) },
  );
}

export function removeRadarrInstance(id: RadarrID) {
  return integrationRequest<RadarrRemovalResult>(`/api/v1/integrations/radarr/instances/${idPath(id)}`, {
    method: "DELETE",
  });
}

export async function getRadarrInstanceOptions(
  id: RadarrID,
  signal?: AbortSignal,
): Promise<RadarrInstanceOptions> {
  const value = await integrationRequest<Partial<RadarrInstanceOptions>>(
    `/api/v1/integrations/radarr/instances/${idPath(id)}/options`,
    { method: "GET", signal },
  );
  return {
    rootFolders: Array.isArray(value.rootFolders) ? value.rootFolders : [],
    qualityProfiles: Array.isArray(value.qualityProfiles) ? value.qualityProfiles : [],
    tags: Array.isArray(value.tags) ? value.tags : [],
  };
}

export async function listRadarrPresets(signal?: AbortSignal) {
  const value = await integrationRequest<ItemEnvelope<RadarrPreset>>(
    "/api/v1/integrations/radarr/presets",
    { method: "GET", signal },
  );
  return itemsFrom(value);
}

export function createRadarrPreset(draft: RadarrPresetDraft) {
  return integrationRequest<RadarrPreset>("/api/v1/integrations/radarr/presets", {
    method: "POST",
    body: JSON.stringify(draft),
  });
}

export function updateRadarrPreset(id: RadarrID, draft: RadarrPresetDraft) {
  return integrationRequest<RadarrPreset>(
    `/api/v1/integrations/radarr/presets/${idPath(id)}`,
    { method: "PUT", body: JSON.stringify(draft) },
  );
}

export function removeRadarrPreset(id: RadarrID) {
  return integrationRequest<RadarrRemovalResult>(`/api/v1/integrations/radarr/presets/${idPath(id)}`, {
    method: "DELETE",
  });
}

export async function listRadarrWebhooks(signal?: AbortSignal) {
  const value = await integrationRequest<ItemEnvelope<RadarrWebhook>>(
    "/api/v1/integrations/radarr/webhooks",
    { method: "GET", signal },
  );
  return itemsFrom(value);
}

export function createRadarrWebhook(draft: RadarrWebhookDraft) {
  return integrationRequest<RadarrWebhook>("/api/v1/integrations/radarr/webhooks", {
    method: "POST",
    body: JSON.stringify(draft),
  });
}

export function updateRadarrWebhook(id: RadarrID, draft: RadarrWebhookDraft) {
  return integrationRequest<RadarrWebhook>(
    `/api/v1/integrations/radarr/webhooks/${idPath(id)}`,
    { method: "PUT", body: JSON.stringify(draft) },
  );
}

export function archiveRadarrWebhook(id: RadarrID) {
  return integrationRequest<void>(`/api/v1/integrations/radarr/webhooks/${idPath(id)}`, {
    method: "DELETE",
  });
}

export function testRadarrWebhook(id: RadarrID) {
  return integrationRequest<RadarrWebhook>(
    `/api/v1/integrations/radarr/webhooks/${idPath(id)}/test`,
    { method: "POST" },
  );
}

export function testRadarrWebhookDraft(draft: RadarrWebhookDraft) {
  return integrationRequest<{ verified?: boolean; reason?: string }>(
    "/api/v1/integrations/radarr/webhooks/test",
    { method: "POST", body: JSON.stringify(draft) },
  );
}
