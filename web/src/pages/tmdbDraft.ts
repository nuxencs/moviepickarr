import type {
  IntegrationSetting,
  TMDBDraftRequest,
  TMDBIntegration,
} from "@/api/integrations";

export type DurationUnit = "milliseconds" | "seconds" | "minutes" | "hours" | "days";

export interface DurationDraft {
  amount: string;
  unit: DurationUnit;
}

export interface TMDBFormDraft {
  enabled: boolean;
  apiKey: string;
  clearApiKey: boolean;
  castLimit: string;
  allCast: boolean;
  refreshEnabled: boolean;
  refreshInterval: DurationDraft;
  ttl: DurationDraft;
  minInterval: DurationDraft;
  maxRetries: string;
  backoff: DurationDraft;
  batchLimit: string;
  removals: string[];
}

export interface DraftIssue {
  field: string;
  message: string;
}

export class TMDBDraftValidationError extends Error {
  readonly issues: DraftIssue[];

  constructor(issues: DraftIssue[]) {
    super("Review the highlighted settings.");
    this.name = "TMDBDraftValidationError";
    this.issues = issues;
  }
}

const UNIT_MS: Record<DurationUnit, number> = {
  milliseconds: 1,
  seconds: 1_000,
  minutes: 60_000,
  hours: 3_600_000,
  days: 86_400_000,
};

const REFRESH_UNITS: DurationUnit[] = ["days", "hours", "minutes"];
const TTL_UNITS: DurationUnit[] = ["days", "hours"];
const PACING_UNITS: DurationUnit[] = ["seconds", "milliseconds"];

function durationDraft(milliseconds: number, units: DurationUnit[]): DurationDraft {
  const unit =
    units.find((candidate) => milliseconds % UNIT_MS[candidate] === 0) ??
    units[units.length - 1]!;
  return { amount: String(milliseconds / UNIT_MS[unit]), unit };
}

export function createTMDBDraft(config: TMDBIntegration): TMDBFormDraft {
  const settings = config.settings;
  const castValue = settings.castLimit.value || settings.castLimit.default || 15;
  const refreshValue =
    settings.refreshIntervalMs.value || settings.refreshIntervalMs.default || 3_600_000;

  return {
    enabled: settings.enabled.value,
    apiKey: "",
    clearApiKey: false,
    castLimit: String(castValue),
    allCast: settings.castLimit.value === 0,
    refreshEnabled: settings.refreshIntervalMs.value !== 0,
    refreshInterval: durationDraft(refreshValue, REFRESH_UNITS),
    ttl: durationDraft(settings.ttlMs.value, TTL_UNITS),
    minInterval: durationDraft(settings.minIntervalMs.value, PACING_UNITS),
    maxRetries: String(settings.maxRetries.value),
    backoff: durationDraft(settings.backoffMs.value, PACING_UNITS),
    batchLimit: String(settings.batchLimit.value),
    removals: [],
  };
}

function parseInteger(
  value: string,
  field: string,
  minimum: number,
  issues: DraftIssue[],
): number | undefined {
  const parsed = Number(value);
  if (value.trim() === "" || !Number.isSafeInteger(parsed)) {
    issues.push({ field, message: "Enter a whole number." });
    return undefined;
  }
  if (parsed < minimum) {
    issues.push({ field, message: `Must be at least ${minimum}.` });
    return undefined;
  }
  return parsed;
}

function parseDuration(
  value: DurationDraft,
  field: string,
  minimum: number,
  issues: DraftIssue[],
): number | undefined {
  if (value.amount.trim() === "") {
    issues.push({ field, message: "Enter a duration." });
    return undefined;
  }
  const amount = Number(value.amount);
  const milliseconds = amount * UNIT_MS[value.unit];
  if (!Number.isFinite(amount) || !Number.isSafeInteger(milliseconds)) {
    issues.push({ field, message: "Enter a valid duration." });
    return undefined;
  }
  if (milliseconds < minimum) {
    issues.push({
      field,
      message: minimum === 1 ? "Must be greater than zero." : `Must be at least ${minimum} ms.`,
    });
    return undefined;
  }
  return milliseconds;
}

function adminValue<T>(
  setting: IntegrationSetting<T>,
  value: T | undefined,
  field: string,
  removals: string[],
): T | undefined {
  if (setting.source === "environment" || removals.includes(field) || value === undefined) {
    return undefined;
  }
  if (setting.source === "admin" || !Object.is(value, setting.value)) return value;
  return undefined;
}

export function buildTMDBRequest(
  config: TMDBIntegration,
  draft: TMDBFormDraft,
  confirmWarnings: boolean,
): TMDBDraftRequest {
  const issues: DraftIssue[] = [];
  const settings = config.settings;

  const castLimit = draft.allCast
    ? 0
    : parseInteger(draft.castLimit, "castLimit", 1, issues);
  const refreshInterval = draft.refreshEnabled
    ? parseDuration(draft.refreshInterval, "refreshInterval", 1, issues)
    : 0;
  const ttl = parseDuration(draft.ttl, "ttl", 1, issues);
  const minInterval = parseDuration(draft.minInterval, "minInterval", 0, issues);
  const maxRetries = parseInteger(draft.maxRetries, "maxRetries", 0, issues);
  const backoff = parseDuration(draft.backoff, "backoff", 0, issues);
  const batchLimit = parseInteger(draft.batchLimit, "batchLimit", 1, issues);

  const relevantIssues = issues.filter((issue) => {
    const field = issue.field;
    const setting =
      field === "castLimit"
        ? settings.castLimit
        : field === "refreshInterval"
          ? settings.refreshIntervalMs
          : field === "ttl"
            ? settings.ttlMs
            : field === "minInterval"
              ? settings.minIntervalMs
              : field === "maxRetries"
                ? settings.maxRetries
                : field === "backoff"
                  ? settings.backoffMs
                  : settings.batchLimit;
    return setting.source !== "environment" && !draft.removals.includes(field);
  });
  if (relevantIssues.length > 0) throw new TMDBDraftValidationError(relevantIssues);

  const requestSettings = {
    enabled: adminValue(settings.enabled, draft.enabled, "enabled", draft.removals),
    castLimit: adminValue(settings.castLimit, castLimit, "castLimit", draft.removals),
    refreshIntervalMs: adminValue(
      settings.refreshIntervalMs,
      refreshInterval,
      "refreshInterval",
      draft.removals,
    ),
    ttlMs: adminValue(settings.ttlMs, ttl, "ttl", draft.removals),
    minIntervalMs: adminValue(
      settings.minIntervalMs,
      minInterval,
      "minInterval",
      draft.removals,
    ),
    maxRetries: adminValue(
      settings.maxRetries,
      maxRetries,
      "maxRetries",
      draft.removals,
    ),
    backoffMs: adminValue(settings.backoffMs, backoff, "backoff", draft.removals),
    batchLimit: adminValue(
      settings.batchLimit,
      batchLimit,
      "batchLimit",
      draft.removals,
    ),
  };

  return {
    revision: config.revision,
    settings: Object.fromEntries(
      Object.entries(requestSettings).filter(([, value]) => value !== undefined),
    ),
    removeFallbacks: [...draft.removals],
    apiKey: draft.apiKey.trim(),
    clearApiKey: draft.clearApiKey,
    confirmWarnings,
  };
}

function parsedDuration(value: DurationDraft) {
  if (value.amount.trim() === "") return Number.NaN;
  return Number(value.amount) * UNIT_MS[value.unit];
}

export function draftIsDirty(config: TMDBIntegration, draft: TMDBFormDraft): boolean {
  if (draft.removals.length > 0 || draft.apiKey.trim() !== "" || draft.clearApiKey) return true;
  const settings = config.settings;
  if (settings.enabled.source !== "environment" && draft.enabled !== settings.enabled.value) return true;
  if (
    settings.castLimit.source !== "environment" &&
    (draft.allCast ? 0 : Number(draft.castLimit)) !== settings.castLimit.value
  ) {
    return true;
  }
  if (
    settings.refreshIntervalMs.source !== "environment" &&
    (draft.refreshEnabled ? parsedDuration(draft.refreshInterval) : 0) !==
      settings.refreshIntervalMs.value
  ) {
    return true;
  }
  if (settings.ttlMs.source !== "environment" && parsedDuration(draft.ttl) !== settings.ttlMs.value) {
    return true;
  }
  if (
    settings.minIntervalMs.source !== "environment" &&
    parsedDuration(draft.minInterval) !== settings.minIntervalMs.value
  ) {
    return true;
  }
  if (
    settings.maxRetries.source !== "environment" &&
    Number(draft.maxRetries) !== settings.maxRetries.value
  ) {
    return true;
  }
  if (
    settings.backoffMs.source !== "environment" &&
    parsedDuration(draft.backoff) !== settings.backoffMs.value
  ) {
    return true;
  }
  return (
    settings.batchLimit.source !== "environment" &&
    Number(draft.batchLimit) !== settings.batchLimit.value
  );
}

export const durationUnits = {
  refresh: REFRESH_UNITS,
  ttl: TTL_UNITS,
  pacing: PACING_UNITS,
} as const;
