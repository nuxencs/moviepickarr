import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useBlocker } from "@tanstack/react-router";
import { InfoIcon } from "lucide-react";
import {
  memo,
  useCallback,
  useEffect,
  useId,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from "react";
import { createPortal } from "react-dom";

import {
  IntegrationProblem,
  IntegrationKeys,
  saveTMDBIntegration,
  testTMDBConnection,
  type IntegrationProblemItem,
  type IntegrationSetting,
  type IntegrationSource,
  type SecretIntegrationSetting,
  type TMDBConnectionResult,
  type TMDBDraftRequest,
  type TMDBIntegration,
} from "@/api/integrations";

import { plural } from "@/components/moviepickarr/lib";
import { Modal } from "@/components/moviepickarr/Modal";

import {
  buildTMDBRequest,
  createTMDBDraft,
  draftIsDirty,
  durationUnits,
  TMDBDraftValidationError,
  type DurationDraft,
  type DurationUnit,
  type TMDBFormDraft,
} from "@/pages/tmdbDraft";

type DraftField = Exclude<keyof TMDBFormDraft, "removals">;

const REMOVAL_BY_DRAFT_FIELD: Partial<Record<DraftField, string>> = {
  enabled: "enabled",
  castLimit: "castLimit",
  allCast: "castLimit",
  refreshEnabled: "refreshInterval",
  refreshInterval: "refreshInterval",
  ttl: "ttl",
  minInterval: "minInterval",
  maxRetries: "maxRetries",
  backoff: "backoff",
  batchLimit: "batchLimit",
};

const ADVANCED_FIELDS = new Set(["minInterval", "maxRetries", "backoff", "batchLimit"]);

const UNIT_LABELS: Record<DurationUnit, string> = {
  milliseconds: "milliseconds",
  seconds: "seconds",
  minutes: "minutes",
  hours: "hours",
  days: "days",
};

function sourceLabel(source: IntegrationSource) {
  if (source === "environment") return "Controlled by environment";
  if (source === "admin") return "Source: Admin";
  return "Source: Default";
}

function readableDuration(milliseconds: number) {
  const units: [number, string][] = [
    [86_400_000, "day"],
    [3_600_000, "hour"],
    [60_000, "minute"],
    [1_000, "second"],
    [1, "ms"],
  ];
  for (const [size, name] of units) {
    if (milliseconds >= size && milliseconds % size === 0) {
      const amount = milliseconds / size;
      return name === "ms" ? `${amount} ms` : plural(amount, name);
    }
  }
  return `${milliseconds} ms`;
}

interface SettingHelpProps {
  label: string;
  help: string;
  environment: string;
  defaultLabel: string;
}

interface HelpPlacement {
  top: number;
  left: number;
}

const HELP_GAP = 6;
const HELP_MARGIN = 8;

function SettingHelp({ label, help, environment, defaultLabel }: SettingHelpProps) {
  const [mode, setMode] = useState<"closed" | "transient" | "pinned">("closed");
  const [placement, setPlacement] = useState<HelpPlacement | null>(null);
  const focused = useRef(false);
  const triggerRef = useRef<HTMLButtonElement>(null);
  const tooltipRef = useRef<HTMLSpanElement>(null);
  const tooltipID = useId();
  const open = mode !== "closed";

  const place = useCallback(() => {
    const trigger = triggerRef.current;
    const tooltip = tooltipRef.current;
    if (!trigger || !tooltip) return;

    const triggerRect = trigger.getBoundingClientRect();
    const tooltipRect = tooltip.getBoundingClientRect();
    let top = triggerRect.bottom + HELP_GAP;
    if (
      top + tooltipRect.height > window.innerHeight - HELP_MARGIN &&
      triggerRect.top - HELP_GAP - tooltipRect.height >= HELP_MARGIN
    ) {
      top = triggerRect.top - HELP_GAP - tooltipRect.height;
    }
    const left = Math.max(
      HELP_MARGIN,
      Math.min(triggerRect.left, window.innerWidth - tooltipRect.width - HELP_MARGIN),
    );

    setPlacement((current) =>
      current?.top === top && current.left === left ? current : { top, left },
    );
  }, []);

  useLayoutEffect(() => {
    if (open) place();
  }, [open, place]);

  useEffect(() => {
    if (!open) return;
    let frame: number | null = null;
    const reposition = () => {
      if (frame !== null) return;
      frame = window.requestAnimationFrame(() => {
        frame = null;
        place();
      });
    };
    window.addEventListener("scroll", reposition, true);
    window.addEventListener("resize", reposition);
    return () => {
      if (frame !== null) window.cancelAnimationFrame(frame);
      window.removeEventListener("scroll", reposition, true);
      window.removeEventListener("resize", reposition);
    };
  }, [open, place]);

  return (
    <span
      className="int-help"
      data-open={open}
      onMouseEnter={() => setMode((current) => current === "closed" ? "transient" : current)}
      onMouseLeave={() => {
        if (!focused.current) {
          setMode((current) => current === "transient" ? "closed" : current);
        }
      }}
    >
      <button
        ref={triggerRef}
        type="button"
        className="iconbtn int-help__trigger"
        aria-label={`About ${label}`}
        aria-expanded={open}
        aria-describedby={tooltipID}
        onClick={() => setMode((current) => current === "pinned" ? "closed" : "pinned")}
        onFocus={() => {
          focused.current = true;
          setMode((current) => current === "closed" ? "transient" : current);
        }}
        onBlur={() => {
          focused.current = false;
          setMode("closed");
        }}
        onKeyDown={(event) => {
          if (event.key === "Escape") {
            event.stopPropagation();
            setMode("closed");
          }
        }}
      >
        <InfoIcon aria-hidden="true" />
      </button>
      {createPortal(
        <span
          ref={tooltipRef}
          id={tooltipID}
          className="int-help__tooltip"
          role="tooltip"
          hidden={!open}
          style={{
            top: placement?.top ?? 0,
            left: placement?.left ?? 0,
            visibility: open && placement ? "visible" : "hidden",
          }}
        >
          <span>{help}</span>
          <code>{environment}</code>
          <span>Default: {defaultLabel}</span>
        </span>,
        document.body,
      )}
    </span>
  );
}

interface SettingRowProps<T> {
  id: string;
  label: string;
  help: string;
  setting: IntegrationSetting<T>;
  defaultLabel: string;
  staged: boolean;
  warning?: string;
  issue?: string;
  onUseDefault: () => void;
  onUndoDefault: () => void;
  children: ReactNode;
}

function SettingRow<T>({
  id,
  label,
  help,
  setting,
  defaultLabel,
  staged,
  warning,
  issue,
  onUseDefault,
  onUndoDefault,
  children,
}: SettingRowProps<T>) {
  const canRemove = setting.source === "admin" || setting.hasAdminFallback;
  const removeLabel = setting.source === "environment" ? "Remove saved fallback" : "Use default";

  return (
    <div className="int-setting" role="group" aria-labelledby={`${id}-label`}>
      <div className="int-setting__label">
        <h4 id={`${id}-label`}>{label}</h4>
        <SettingHelp
          label={label}
          help={help}
          environment={setting.environment}
          defaultLabel={defaultLabel}
        />
      </div>
      <span className="int-source" data-source={setting.source}>
        {sourceLabel(setting.source)}
      </span>
      <div className="int-setting__value">
        <div className="int-setting__control">{children}</div>
        {warning ? <p className="int-setting__warning">{warning}</p> : null}
        {issue ? (
          <p id={`${id}-error`} className="field-error" role="alert">
            {issue}
          </p>
        ) : null}
        {staged ? (
          <div className="int-setting__pending">
            <span>
              {setting.source === "environment"
                ? "Saved fallback will be removed on save."
                : "The built-in default will be used after save."}
            </span>
            <button type="button" className="btn btn--ghost btn--sm int-setting-action" onClick={onUndoDefault}>
              Undo
            </button>
          </div>
        ) : canRemove ? (
          <button type="button" className="btn btn--ghost btn--sm int-setting-action" onClick={onUseDefault}>
            {removeLabel}
          </button>
        ) : null}
      </div>
    </div>
  );
}

interface DurationControlProps {
  amountLabel: string;
  unitLabel: string;
  value: DurationDraft;
  units: readonly DurationUnit[];
  disabled: boolean;
  invalid?: boolean;
  describedBy?: string;
  minimum?: number;
  onChange: (value: DurationDraft) => void;
}

function DurationControl({
  amountLabel,
  unitLabel,
  value,
  units,
  disabled,
  invalid,
  describedBy,
  minimum = 0,
  onChange,
}: DurationControlProps) {
  return (
    <div className="int-duration">
      <label className="field int-field int-field--number" data-invalid={invalid ? true : undefined}>
        <input
          aria-label={amountLabel}
          type="number"
          min={minimum}
          step="any"
          value={value.amount}
          disabled={disabled}
          aria-invalid={invalid ? true : undefined}
          aria-describedby={describedBy}
          onChange={(event) => onChange({ ...value, amount: event.target.value })}
        />
      </label>
      <label className="field int-field int-field--select" data-invalid={invalid ? true : undefined}>
        <select
          aria-label={unitLabel}
          value={value.unit}
          disabled={disabled}
          aria-invalid={invalid ? true : undefined}
          aria-describedby={describedBy}
          onChange={(event) => onChange({ ...value, unit: event.target.value as DurationUnit })}
        >
          {units.map((unit) => (
            <option key={unit} value={unit}>
              {UNIT_LABELS[unit]}
            </option>
          ))}
        </select>
      </label>
    </div>
  );
}

function defaultDraftFor<T extends keyof TMDBIntegration["settings"]>(
  config: TMDBIntegration,
  key: T,
) {
  const setting = config.settings[key];
  if (!("default" in setting) || setting.source === "environment") {
    return createTMDBDraft(config);
  }
  return createTMDBDraft({
    ...config,
    settings: {
      ...config.settings,
      [key]: { ...setting, value: setting.default },
    },
  });
}

interface SecretRowProps {
  setting: SecretIntegrationSetting;
  draft: TMDBFormDraft;
  setDraft: (next: (current: TMDBFormDraft) => TMDBFormDraft) => void;
  issue?: string;
}

function SecretRow({ setting, draft, setDraft, issue }: SecretRowProps) {
  const environmentControlled = setting.source === "environment";
  const canClear = setting.source === "admin" || setting.hasAdminFallback;
  return (
    <div className="int-setting" role="group" aria-labelledby="api-key-label">
      <div className="int-setting__label">
        <h4 id="api-key-label">API key</h4>
        <SettingHelp
          label="API key"
          help="Credential used to connect to TMDB. Stored encrypted and never shown again."
          environment={setting.environment}
          defaultLabel="Not configured"
        />
      </div>
      <span className="int-source" data-source={setting.source}>
        {sourceLabel(setting.source)}
      </span>
      <div className="int-setting__value">
        <div className="int-secret">
          <label className="field" data-invalid={issue ? true : undefined}>
            <span className="vis-hidden">New API key</span>
            <input
              aria-label="New API key"
              type="password"
              autoComplete="new-password"
              placeholder={setting.configured ? "Enter a replacement key" : "Enter API key"}
              value={draft.apiKey}
              disabled={environmentControlled}
              aria-invalid={issue ? true : undefined}
              aria-describedby={issue ? "api-key-error" : undefined}
              onChange={(event) =>
                setDraft((current) => ({
                  ...current,
                  apiKey: event.target.value,
                  clearApiKey: false,
                }))
              }
            />
          </label>
          <span className="int-secret__state">
            {setting.configured ? "Configured" : "Not configured"}
          </span>
        </div>
        {issue ? (
          <p id="api-key-error" className="field-error" role="alert">
            {issue}
          </p>
        ) : null}
        {draft.clearApiKey ? (
          <div className="int-setting__pending">
            <span>
              {environmentControlled
                ? "Saved fallback will be removed on save."
                : "The saved API key will be cleared on save."}
            </span>
            <button
              type="button"
              className="btn btn--ghost btn--sm int-setting-action"
              onClick={() => setDraft((current) => ({ ...current, clearApiKey: false }))}
            >
              Undo
            </button>
          </div>
        ) : canClear ? (
          <button
            type="button"
            className="btn btn--ghost btn--sm int-setting-action"
            onClick={() =>
              setDraft((current) => ({ ...current, apiKey: "", clearApiKey: true }))
            }
          >
            {environmentControlled ? "Remove saved fallback" : "Clear API key"}
          </button>
        ) : null}
      </div>
    </div>
  );
}

interface TMDBSettingsFormProps {
  serverConfig: TMDBIntegration;
}

const TEST_STATE_LABELS: Record<TMDBConnectionResult["state"], string> = {
  disabled: "Disabled",
  connected: "Connected",
  could_not_verify: "Could not verify",
  error: "Error",
  credential_unavailable: "Credential unavailable",
};

const TEST_MONTH_FORMATTER = new Intl.DateTimeFormat("en-US", {
  month: "short",
  timeZone: "UTC",
});
const TEST_TIME_FORMATTER = new Intl.DateTimeFormat("en-US", {
  hour: "numeric",
  minute: "2-digit",
  timeZone: "UTC",
});

function checkedAtLabel(iso: string) {
  const value = new Date(iso);
  if (Number.isNaN(value.getTime())) return "Checked just now";
  const month = TEST_MONTH_FORMATTER.format(value);
  const time = TEST_TIME_FORMATTER.format(value);
  return `Checked ${month} ${value.getUTCDate()}, ${value.getUTCFullYear()} at ${time} UTC`;
}

function toIssueMap(items: IntegrationProblemItem[]) {
  return Object.fromEntries(items.map((item) => [item.field, item.message]));
}

export const TMDBSettingsForm = memo(function TMDBSettingsForm({
  serverConfig,
}: TMDBSettingsFormProps) {
  const queryClient = useQueryClient();
  const [baseConfig, setBaseConfig] = useState(serverConfig);
  const [draft, setDraftState] = useState(() => createTMDBDraft(serverConfig));
  const [issues, setIssues] = useState<Record<string, string>>({});
  const advancedRef = useRef<HTMLDetailsElement>(null);
  const [feedback, setFeedback] = useState("");
  const [savedMessage, setSavedMessage] = useState("");
  const [testResult, setTestResult] = useState<TMDBConnectionResult | null>(null);
  const [incomingConfig, setIncomingConfig] = useState<TMDBIntegration | null>(null);
  const [warningRequest, setWarningRequest] = useState<{
    request: TMDBDraftRequest;
    warnings: IntegrationProblemItem[];
  } | null>(null);
  const settings = baseConfig.settings;
  const initialDraft = useMemo(() => createTMDBDraft(baseConfig), [baseConfig]);
  const setDraft = (next: (current: TMDBFormDraft) => TMDBFormDraft) => {
    setDraftState(next);
    setSavedMessage("");
    setTestResult(null);
  };
  const patch = <K extends DraftField>(key: K, value: TMDBFormDraft[K]) => {
    const removal = REMOVAL_BY_DRAFT_FIELD[key];
    setDraft((current) => ({
      ...current,
      [key]: value,
      removals: removal
        ? current.removals.filter((candidate) => candidate !== removal)
        : current.removals,
    }));
  };
  const staged = (field: string) => draft.removals.includes(field);
  const stage = (field: string, patchDraft: Partial<TMDBFormDraft>) =>
    setDraft((current) => ({
      ...current,
      ...patchDraft,
      removals: current.removals.includes(field)
        ? current.removals
        : [...current.removals, field],
    }));
  const undo = (field: string) =>
    setDraft((current) => {
      const restored: TMDBFormDraft = {
        ...current,
        removals: current.removals.filter((candidate) => candidate !== field),
      };
      switch (field) {
        case "enabled":
          restored.enabled = initialDraft.enabled;
          break;
        case "castLimit":
          restored.castLimit = initialDraft.castLimit;
          restored.allCast = initialDraft.allCast;
          break;
        case "refreshInterval":
          restored.refreshEnabled = initialDraft.refreshEnabled;
          restored.refreshInterval = initialDraft.refreshInterval;
          break;
        case "ttl":
          restored.ttl = initialDraft.ttl;
          break;
        case "minInterval":
          restored.minInterval = initialDraft.minInterval;
          break;
        case "maxRetries":
          restored.maxRetries = initialDraft.maxRetries;
          break;
        case "backoff":
          restored.backoff = initialDraft.backoff;
          break;
        case "batchLimit":
          restored.batchLimit = initialDraft.batchLimit;
          break;
      }
      return restored;
    });

  const defaults = useMemo(
    () => ({
      enabled: defaultDraftFor(baseConfig, "enabled"),
      cast: defaultDraftFor(baseConfig, "castLimit"),
      refresh: defaultDraftFor(baseConfig, "refreshIntervalMs"),
      ttl: defaultDraftFor(baseConfig, "ttlMs"),
      min: defaultDraftFor(baseConfig, "minIntervalMs"),
      retries: defaultDraftFor(baseConfig, "maxRetries"),
      backoff: defaultDraftFor(baseConfig, "backoffMs"),
      batch: defaultDraftFor(baseConfig, "batchLimit"),
    }),
    [baseConfig],
  );
  const dirty = draftIsDirty(baseConfig, draft);
  const blocker = useBlocker({
    shouldBlockFn: () => dirty,
    enableBeforeUnload: dirty,
    withResolver: true,
  });
  useEffect(() => {
    if (!dirty) return;
    const guard = (event: BeforeUnloadEvent) => {
      event.preventDefault();
      event.returnValue = "";
    };
    window.addEventListener("beforeunload", guard);
    return () => window.removeEventListener("beforeunload", guard);
  }, [dirty]);
  useEffect(() => {
    const hasAdvancedIssue = Object.keys(issues).some((field) => ADVANCED_FIELDS.has(field));
    if (hasAdvancedIssue && advancedRef.current) advancedRef.current.open = true;
  }, [issues]);
  useEffect(() => {
    if (serverConfig.revision === baseConfig.revision) return;
    if (dirty) {
      setIncomingConfig(serverConfig);
      return;
    }
    setBaseConfig(serverConfig);
    setDraftState(createTMDBDraft(serverConfig));
    setIncomingConfig(null);
    setIssues({});
    setFeedback("");
  }, [baseConfig.revision, dirty, serverConfig]);
  const testConnection = useMutation({
    mutationFn: testTMDBConnection,
    onSuccess: (result) => {
      setIssues({});
      setFeedback("");
      setTestResult(result);
      void queryClient.invalidateQueries({ queryKey: IntegrationKeys.tmdb() });
    },
    onError: (error) => {
      setTestResult(null);
      if (error instanceof IntegrationProblem) {
        setIssues(toIssueMap(error.issues));
        setFeedback(error.message);
      } else {
        setFeedback("The connection test could not be completed.");
      }
    },
  });
  const acceptSavedConfig = (saved: TMDBIntegration) => {
    setBaseConfig(saved);
    setDraftState(createTMDBDraft(saved));
    setIssues({});
    setFeedback("");
    setSavedMessage("Changes saved.");
    setWarningRequest(null);
    setIncomingConfig(null);
    queryClient.setQueryData(IntegrationKeys.tmdb(), saved);
    void queryClient.invalidateQueries({ queryKey: IntegrationKeys.runHistory() });
  };
  const saveChanges = useMutation({
    mutationFn: saveTMDBIntegration,
    onSuccess: acceptSavedConfig,
    onError: (error, request) => {
      setSavedMessage("");
      if (error instanceof IntegrationProblem) {
        if (error.title === "confirmation_required") {
          setWarningRequest({ request, warnings: error.warnings });
          return;
        }
        if (error.title === "stale_revision") {
          setFeedback("Another admin changed these settings. Your unsaved draft is still here.");
          void queryClient.invalidateQueries({ queryKey: IntegrationKeys.tmdb() });
          return;
        }
        setIssues(toIssueMap(error.issues));
        setFeedback(error.message);
        return;
      }
      setFeedback("TMDB settings could not be saved.");
    },
  });

  const runTest = () => {
    setFeedback("");
    setIssues({});
    try {
      testConnection.mutate(buildTMDBRequest(baseConfig, draft, false));
    } catch (error) {
      if (error instanceof TMDBDraftValidationError) {
        setIssues(toIssueMap(error.issues));
        setFeedback(error.message);
      }
    }
  };
  const submit = () => {
    setFeedback("");
    setSavedMessage("");
    setIssues({});
    try {
      saveChanges.mutate(buildTMDBRequest(baseConfig, draft, false));
    } catch (error) {
      if (error instanceof TMDBDraftValidationError) {
        setIssues(toIssueMap(error.issues));
        setFeedback(error.message);
      }
    }
  };

  return (
    <form
      className="int-form"
      noValidate
      onSubmit={(event) => {
        event.preventDefault();
        submit();
      }}
    >
      <div className="int-form__head">
        <h3>Configuration</h3>
        <span className="int-revision">Revision {baseConfig.revision}</span>
      </div>

      <div className="int-settings" aria-label="Standard settings">
        <SettingRow
          id="enabled"
          label="Enabled"
          help="Allow TMDB searches and metadata fetching."
          setting={settings.enabled}
          defaultLabel={settings.enabled.default ? "Enabled" : "Disabled"}
          staged={staged("enabled")}
          issue={issues.enabled}
          onUseDefault={() => stage("enabled", { enabled: defaults.enabled.enabled })}
          onUndoDefault={() => undo("enabled")}
        >
          <label className="int-toggle">
            <input
              type="checkbox"
              checked={draft.enabled}
              disabled={settings.enabled.source === "environment"}
              aria-invalid={issues.enabled ? true : undefined}
              aria-describedby={issues.enabled ? "enabled-error" : undefined}
              onChange={(event) => patch("enabled", event.target.checked)}
            />
            <span>Enabled</span>
          </label>
        </SettingRow>

        <SecretRow
          setting={settings.apiKey}
          draft={draft}
          setDraft={setDraft}
          issue={issues.apiKey}
        />

        <SettingRow
          id="cast-limit"
          label="Cast limit"
          help="Maximum cast members stored for each movie."
          setting={settings.castLimit}
          defaultLabel={settings.castLimit.default === 0 ? "All" : String(settings.castLimit.default)}
          staged={staged("castLimit")}
          issue={issues.castLimit}
          onUseDefault={() =>
            stage("castLimit", {
              castLimit: defaults.cast.castLimit,
              allCast: defaults.cast.allCast,
            })
          }
          onUndoDefault={() => undo("castLimit")}
        >
          <div className="int-number-toggle">
            <label className="field int-field int-field--number" data-invalid={issues.castLimit ? true : undefined}>
              <input
                aria-label="Cast limit"
                type="number"
                min="1"
                step="1"
                value={draft.castLimit}
                disabled={draft.allCast || settings.castLimit.source === "environment"}
                aria-invalid={issues.castLimit ? true : undefined}
                aria-describedby={issues.castLimit ? "cast-limit-error" : undefined}
                onChange={(event) => patch("castLimit", event.target.value)}
              />
            </label>
            <label className="int-toggle">
              <input
                type="checkbox"
                checked={draft.allCast}
                disabled={settings.castLimit.source === "environment"}
                aria-describedby={issues.castLimit ? "cast-limit-error" : undefined}
                onChange={(event) => patch("allCast", event.target.checked)}
              />
              <span>All cast members</span>
            </label>
          </div>
        </SettingRow>

        <SettingRow
          id="refresh-interval"
          label="Scheduled refresh"
          help="How often missing or stale TMDB data is checked."
          setting={settings.refreshIntervalMs}
          defaultLabel={readableDuration(settings.refreshIntervalMs.default)}
          staged={staged("refreshInterval")}
          issue={issues.refreshInterval}
          warning={
            settings.refreshIntervalMs.source === "environment" &&
            settings.refreshIntervalMs.value > 0 &&
            settings.refreshIntervalMs.value < 900_000
              ? "Below the recommended 15 minute minimum."
              : undefined
          }
          onUseDefault={() =>
            stage("refreshInterval", {
              refreshEnabled: defaults.refresh.refreshEnabled,
              refreshInterval: defaults.refresh.refreshInterval,
            })
          }
          onUndoDefault={() => undo("refreshInterval")}
        >
          <div className="int-number-toggle">
            <DurationControl
              amountLabel="Refresh every"
              unitLabel="Refresh unit"
              value={draft.refreshInterval}
              units={durationUnits.refresh}
              disabled={!draft.refreshEnabled || settings.refreshIntervalMs.source === "environment"}
              invalid={Boolean(issues.refreshInterval)}
              describedBy={issues.refreshInterval ? "refresh-interval-error" : undefined}
              minimum={1}
              onChange={(value) => patch("refreshInterval", value)}
            />
            <label className="int-toggle">
              <input
                type="checkbox"
                checked={draft.refreshEnabled}
                disabled={settings.refreshIntervalMs.source === "environment"}
                aria-describedby={issues.refreshInterval ? "refresh-interval-error" : undefined}
                onChange={(event) => patch("refreshEnabled", event.target.checked)}
              />
              <span>Scheduled refresh</span>
            </label>
          </div>
        </SettingRow>

        <SettingRow
          id="ttl"
          label="Metadata freshness"
          help="How old cached TMDB data may become before it is stale."
          setting={settings.ttlMs}
          defaultLabel={readableDuration(settings.ttlMs.default)}
          staged={staged("ttl")}
          issue={issues.ttl}
          warning={
            settings.ttlMs.source === "environment" && settings.ttlMs.value < 3_600_000
              ? "Below the recommended 1 hour minimum."
              : undefined
          }
          onUseDefault={() => stage("ttl", { ttl: defaults.ttl.ttl })}
          onUndoDefault={() => undo("ttl")}
        >
          <DurationControl
            amountLabel="Metadata freshness"
            unitLabel="Metadata freshness unit"
            value={draft.ttl}
            units={durationUnits.ttl}
            disabled={settings.ttlMs.source === "environment"}
            invalid={Boolean(issues.ttl)}
            describedBy={issues.ttl ? "ttl-error" : undefined}
            onChange={(value) => patch("ttl", value)}
          />
        </SettingRow>
      </div>

      <details ref={advancedRef} className="int-advanced">
        <summary>Advanced</summary>
        <div className="int-settings">
          <SettingRow
            id="min-interval"
            label="Request interval"
            help="Minimum pause between TMDB requests."
            setting={settings.minIntervalMs}
            defaultLabel={readableDuration(settings.minIntervalMs.default)}
            staged={staged("minInterval")}
            issue={issues.minInterval}
            warning={
              settings.minIntervalMs.source === "environment" && settings.minIntervalMs.value < 250
                ? "Below the recommended 250 ms minimum."
                : undefined
            }
            onUseDefault={() => stage("minInterval", { minInterval: defaults.min.minInterval })}
            onUndoDefault={() => undo("minInterval")}
          >
            <DurationControl
              amountLabel="Request interval"
              unitLabel="Request interval unit"
              value={draft.minInterval}
              units={durationUnits.pacing}
              disabled={settings.minIntervalMs.source === "environment"}
              invalid={Boolean(issues.minInterval)}
              describedBy={issues.minInterval ? "min-interval-error" : undefined}
              onChange={(value) => patch("minInterval", value)}
            />
          </SettingRow>

          <SettingRow
            id="max-retries"
            label="Retry attempts"
            help="Extra attempts after a temporary TMDB failure."
            setting={settings.maxRetries}
            defaultLabel={String(settings.maxRetries.default)}
            staged={staged("maxRetries")}
            issue={issues.maxRetries}
            onUseDefault={() => stage("maxRetries", { maxRetries: defaults.retries.maxRetries })}
            onUndoDefault={() => undo("maxRetries")}
          >
            <label className="field int-field int-field--number" data-invalid={issues.maxRetries ? true : undefined}>
              <input
                aria-label="Retry attempts"
                type="number"
                min="0"
                step="1"
                value={draft.maxRetries}
                disabled={settings.maxRetries.source === "environment"}
                aria-invalid={issues.maxRetries ? true : undefined}
                aria-describedby={issues.maxRetries ? "max-retries-error" : undefined}
                onChange={(event) => patch("maxRetries", event.target.value)}
              />
            </label>
          </SettingRow>

          <SettingRow
            id="backoff"
            label="Retry backoff"
            help="Initial wait before retrying a temporary failure."
            setting={settings.backoffMs}
            defaultLabel={readableDuration(settings.backoffMs.default)}
            staged={staged("backoff")}
            issue={issues.backoff}
            onUseDefault={() => stage("backoff", { backoff: defaults.backoff.backoff })}
            onUndoDefault={() => undo("backoff")}
          >
            <DurationControl
              amountLabel="Retry backoff"
              unitLabel="Retry backoff unit"
              value={draft.backoff}
              units={durationUnits.pacing}
              disabled={settings.backoffMs.source === "environment"}
              invalid={Boolean(issues.backoff)}
              describedBy={issues.backoff ? "backoff-error" : undefined}
              onChange={(value) => patch("backoff", value)}
            />
          </SettingRow>

          <SettingRow
            id="batch-limit"
            label="Batch size"
            help="Maximum movies selected for one scheduled or manual refresh."
            setting={settings.batchLimit}
            defaultLabel={String(settings.batchLimit.default)}
            staged={staged("batchLimit")}
            issue={issues.batchLimit}
            onUseDefault={() => stage("batchLimit", { batchLimit: defaults.batch.batchLimit })}
            onUndoDefault={() => undo("batchLimit")}
          >
            <label className="field int-field int-field--number" data-invalid={issues.batchLimit ? true : undefined}>
              <input
                aria-label="Batch size"
                type="number"
                min="1"
                step="1"
                value={draft.batchLimit}
                disabled={settings.batchLimit.source === "environment"}
                aria-invalid={issues.batchLimit ? true : undefined}
                aria-describedby={issues.batchLimit ? "batch-limit-error" : undefined}
                onChange={(event) => patch("batchLimit", event.target.value)}
              />
            </label>
          </SettingRow>
        </div>
      </details>

      {feedback ? (
        <p className="int-feedback" role="alert">
          {feedback}
        </p>
      ) : null}
      {incomingConfig ? (
        <div className="int-concurrent">
          <span>Revision {incomingConfig.revision} is now available.</span>
          <button
            type="button"
            className="btn btn--ghost btn--sm int-setting-action"
            onClick={() => {
              setBaseConfig(incomingConfig);
              setDraftState(createTMDBDraft(incomingConfig));
              setIncomingConfig(null);
              setIssues({});
              setFeedback("");
              setSavedMessage("");
            }}
          >
            Load revision {incomingConfig.revision}
          </button>
        </div>
      ) : null}
      {savedMessage ? (
        <p className="int-saved" role="status">
          {savedMessage}
        </p>
      ) : null}
      {testResult ? (
        <div className="int-test-result" role="status" aria-label="Connection test result">
          <span className="int-state" data-state={testResult.state}>
            {TEST_STATE_LABELS[testResult.state]}
          </span>
          {testResult.reason ? <span>{testResult.reason}</span> : null}
          <time dateTime={testResult.checkedAt}>{checkedAtLabel(testResult.checkedAt)}</time>
        </div>
      ) : null}

      <div className="int-form__actions">
        <button
          type="button"
          className="btn btn--ghost"
          disabled={testConnection.isPending || saveChanges.isPending}
          onClick={runTest}
        >
          {testConnection.isPending ? "Testing…" : "Test connection"}
        </button>
        <button
          type="submit"
          className="btn btn--accent"
          disabled={!dirty || saveChanges.isPending || testConnection.isPending}
        >
          {saveChanges.isPending ? "Saving…" : "Save changes"}
        </button>
      </div>

      {warningRequest ? (
        <Modal
          label="Confirm unusual settings"
          className="modal--form"
          dismissible={!saveChanges.isPending}
          onClose={() => setWarningRequest(null)}
        >
          {(close) => (
            <div className="adm-sheet">
              <h3 className="adm-modal__title">Confirm unusual settings</h3>
              <p className="adm-modal__sub">
                These values can increase TMDB traffic or refresh work. Review them before saving.
              </p>
              <ul className="int-warning-list">
                {warningRequest.warnings.map((warning) => (
                  <li key={`${warning.field}:${warning.message}`}>{warning.message}</li>
                ))}
              </ul>
              <div className="adm-modal__actions">
                <button
                  type="button"
                  className="btn btn--ghost"
                  disabled={saveChanges.isPending}
                  onClick={close}
                >
                  Keep editing
                </button>
                <button
                  type="button"
                  className="btn btn--accent"
                  disabled={saveChanges.isPending}
                  onClick={() =>
                    saveChanges.mutate({ ...warningRequest.request, confirmWarnings: true })
                  }
                >
                  {saveChanges.isPending ? "Saving…" : "Save anyway"}
                </button>
              </div>
            </div>
          )}
        </Modal>
      ) : null}
      {blocker.status === "blocked" ? (
        <Modal
          label="Discard unsaved changes?"
          className="modal--form"
          onClose={() => blocker.reset()}
        >
          {() => (
            <div className="adm-sheet">
              <h3 className="adm-modal__title">Discard unsaved changes?</h3>
              <p className="adm-modal__sub">
                Your TMDB draft has not been saved. Leaving will discard it.
              </p>
              <div className="adm-modal__actions">
                <button
                  type="button"
                  className="btn btn--ghost"
                  onClick={() => blocker.reset()}
                >
                  Keep editing
                </button>
                <button
                  type="button"
                  className="btn btn--danger"
                  onClick={() => blocker.proceed()}
                >
                  Leave page
                </button>
              </div>
            </div>
          )}
        </Modal>
      ) : null}
    </form>
  );
}, (previous, next) =>
  previous.serverConfig.revision === next.serverConfig.revision,
);
