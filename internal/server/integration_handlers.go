package server

import (
	"context"
	"errors"
	"math"
	"time"

	"moviepickarr/internal/integration"
	"moviepickarr/internal/integration/tmdb"

	"github.com/gofiber/fiber/v2"
)

type integrationSummaryResponse struct {
	ID             string                                `json:"id"`
	Name           string                                `json:"name"`
	State          string                                `json:"state"`
	Reason         string                                `json:"reason,omitempty"`
	LatestActivity string                                `json:"latestActivity,omitempty"`
	AttentionCount *int                                  `json:"attentionCount,omitzero"`
	Operations     []integrationOperationSummaryResponse `json:"operations"`
}

type integrationOperationSummaryResponse struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type settingResponse[T any] struct {
	Value            T      `json:"value"`
	Source           string `json:"source"`
	Default          T      `json:"default"`
	HasAdminFallback bool   `json:"hasAdminFallback"`
	Environment      string `json:"environment"`
}

type secretSettingResponse struct {
	Configured       bool   `json:"configured"`
	Source           string `json:"source"`
	HasAdminFallback bool   `json:"hasAdminFallback"`
	Environment      string `json:"environment"`
}

type tmdbSettingsResponse struct {
	Enabled         settingResponse[bool]  `json:"enabled"`
	APIKey          secretSettingResponse  `json:"apiKey"`
	CastLimit       settingResponse[int]   `json:"castLimit"`
	RefreshInterval settingResponse[int64] `json:"refreshIntervalMs"`
	TTL             settingResponse[int64] `json:"ttlMs"`
	MinInterval     settingResponse[int64] `json:"minIntervalMs"`
	MaxRetries      settingResponse[int]   `json:"maxRetries"`
	Backoff         settingResponse[int64] `json:"backoffMs"`
	BatchLimit      settingResponse[int]   `json:"batchLimit"`
}

type tmdbIntegrationResponse struct {
	Revision               int64                   `json:"revision"`
	State                  string                  `json:"state"`
	Reason                 string                  `json:"reason,omitempty"`
	Warnings               []tmdb.ConfigWarning    `json:"warnings"`
	Settings               tmdbSettingsResponse    `json:"settings"`
	LatestRun              *integrationRunResponse `json:"latestRun,omitzero"`
	LastCheckedAt          string                  `json:"lastCheckedAt,omitempty"`
	LastConnectionTestedAt string                  `json:"lastConnectionTestedAt,omitempty"`
	NextCheckAt            string                  `json:"nextCheckAt,omitempty"`
	LastSuccessfulRunAt    string                  `json:"lastSuccessfulRunAt,omitempty"`
}

type tmdbSettingsDraftRequest struct {
	Enabled           *bool  `json:"enabled"`
	CastLimit         *int   `json:"castLimit"`
	RefreshIntervalMS *int64 `json:"refreshIntervalMs"`
	TTLMS             *int64 `json:"ttlMs"`
	MinIntervalMS     *int64 `json:"minIntervalMs"`
	MaxRetries        *int   `json:"maxRetries"`
	BackoffMS         *int64 `json:"backoffMs"`
	BatchLimit        *int   `json:"batchLimit"`
}

type tmdbDraftRequest struct {
	Revision        int64                    `json:"revision"`
	Settings        tmdbSettingsDraftRequest `json:"settings"`
	RemoveFallbacks []string                 `json:"removeFallbacks"`
	APIKey          string                   `json:"apiKey"`
	ClearAPIKey     bool                     `json:"clearApiKey"`
	ConfirmWarnings bool                     `json:"confirmWarnings"`
}

type connectionResultResponse struct {
	State     string `json:"state"`
	Reason    string `json:"reason,omitempty"`
	CheckedAt string `json:"checkedAt"`
}

type integrationProblemDetails struct {
	problemDetails
	Issues   []tmdb.ValidationIssue `json:"issues,omitempty"`
	Warnings []tmdb.ConfigWarning   `json:"warnings,omitempty"`
}

func (h *handler) handleListIntegrations(c *fiber.Ctx) error {
	if ok, err := h.requireAdmin(c); !ok {
		return err
	}
	view, err := h.tmdbIntegration.Get(c.UserContext())
	if err != nil {
		return h.writeInternal(c, err, "reading integrations failed")
	}
	row := integrationSummaryResponse{
		ID:     "tmdb",
		Name:   "TMDB",
		State:  string(view.State),
		Reason: view.Reason,
		Operations: []integrationOperationSummaryResponse{
			{ID: string(integration.RunOperationRefreshStale), Name: "Refresh stale"},
			{ID: string(integration.RunOperationReEnrichAll), Name: "Re-enrich all"},
			{ID: string(integration.RunOperationEnrichMovie), Name: "Enrich movie"},
		},
	}
	latestActivity := view.LastCheckedAt
	if view.LastConnectionTestedAt != nil &&
		(latestActivity == nil || view.LastConnectionTestedAt.After(*latestActivity)) {
		latestActivity = view.LastConnectionTestedAt
	}
	if h.integrationRuns != nil {
		latest, err := h.integrationRuns.Latest(c.UserContext(), "tmdb")
		if err != nil {
			return h.writeInternal(c, err, "reading latest integration run failed")
		}
		if latest != nil {
			activityAt := latest.StartedAt
			if latest.FinishedAt != nil {
				activityAt = *latest.FinishedAt
			}
			if latestActivity == nil || activityAt.After(*latestActivity) {
				latestActivity = &activityAt
			}
		}
	}
	if latestActivity != nil {
		row.LatestActivity = latestActivity.UTC().Format(time.RFC3339)
	}
	rows := []integrationSummaryResponse{row}
	if h.radarr != nil {
		radarrRow, err := h.radarrIntegrationSummary(c.UserContext())
		if err != nil {
			return h.writeInternal(c, err, "reading Radarr integration summary failed")
		}
		rows = append(rows, radarrRow)
	}
	return c.Status(fiber.StatusOK).JSON(rows)
}

func (h *handler) radarrIntegrationSummary(ctx context.Context) (integrationSummaryResponse, error) {
	instances, err := h.radarr.listInstances(ctx)
	if err != nil {
		return integrationSummaryResponse{}, err
	}
	attention, err := h.radarr.attentionCount(ctx)
	if err != nil {
		return integrationSummaryResponse{}, err
	}
	row := integrationSummaryResponse{
		ID: "radarr", Name: "Radarr", State: string(integration.StateDisabled),
		AttentionCount: &attention, Operations: []integrationOperationSummaryResponse{},
	}
	active := 0
	connected := false
	credentialUnavailable := false
	offline := false
	credentialReason := ""
	offlineReason := ""
	var latest *time.Time
	for i := range instances {
		instance := instances[i]
		if instance.ArchivedAt != nil {
			continue
		}
		active++
		checkedAt := instance.LastCheckedAt
		if !checkedAt.IsZero() && (latest == nil || checkedAt.After(*latest)) {
			latest = &checkedAt
		}
		switch instance.State {
		case radarrInstanceConnected:
			connected = true
		case radarrInstanceCredentialUnavailable:
			credentialUnavailable = true
			if credentialReason == "" {
				credentialReason = instance.StateReason
			}
		default:
			offline = true
			if offlineReason == "" {
				offlineReason = instance.StateReason
			}
		}
	}
	switch {
	case active == 0:
		row.State = string(integration.StateDisabled)
		row.Reason = ""
	case credentialUnavailable:
		row.State = string(integration.StateCredentialUnavailable)
		row.Reason = credentialReason
	case offline:
		row.State = string(integration.StateCouldNotVerify)
		row.Reason = offlineReason
	case connected:
		row.State = string(integration.StateConnected)
		row.Reason = ""
	}
	if latest != nil {
		row.LatestActivity = latest.UTC().Format(time.RFC3339)
	}
	return row, nil
}

func (h *handler) handleGetTMDBIntegration(c *fiber.Ctx) error {
	if ok, err := h.requireAdmin(c); !ok {
		return err
	}
	response, err := h.loadTMDBIntegrationResponse(c.UserContext())
	if err != nil {
		return h.writeInternal(c, err, "reading TMDB integration failed")
	}
	return c.Status(fiber.StatusOK).JSON(response)
}

func (h *handler) loadTMDBIntegrationResponse(ctx context.Context) (tmdbIntegrationResponse, error) {
	view, err := h.tmdbIntegration.Get(ctx)
	if err != nil {
		return tmdbIntegrationResponse{}, err
	}
	response := toTMDBIntegrationResponse(view)
	if h.integrationRuns != nil {
		latest, err := h.integrationRuns.CurrentLibrary(ctx, "tmdb")
		if err != nil {
			return tmdbIntegrationResponse{}, err
		}
		if latest == nil {
			latest, err = h.integrationRuns.Latest(ctx, "tmdb")
			if err != nil {
				return tmdbIntegrationResponse{}, err
			}
		}
		if latest != nil {
			mapped := toIntegrationRunResponse(*latest)
			response.LatestRun = &mapped
		}
	}
	return response, nil
}

func (h *handler) handleSaveTMDBIntegration(c *fiber.Ctx) error {
	if ok, err := h.requireAdmin(c); !ok {
		return err
	}
	request, issues, err := parseTMDBDraft(c)
	if err != nil {
		return writeProblem(c, fiber.StatusBadRequest, "invalid_request", "invalid request body")
	}
	if len(issues) > 0 {
		return writeTMDBProblem(c, fiber.StatusUnprocessableEntity, "validation_failed", "TMDB settings are invalid", issues, nil)
	}
	view, err := h.tmdbIntegration.Save(c.UserContext(), request)
	if err != nil {
		return h.writeTMDBIntegrationError(c, err, "saving TMDB integration failed")
	}
	h.applyTMDBRuntimeEffects(c, view)
	response, err := h.loadTMDBIntegrationResponse(c.UserContext())
	if err != nil {
		return h.writeInternal(c, err, "reading saved TMDB integration failed")
	}
	return c.Status(fiber.StatusOK).JSON(response)
}

func (h *handler) applyTMDBRuntimeEffects(c *fiber.Ctx, view tmdb.ConfigView) {
	if h.posterWall != nil &&
		view.Config.Enabled.Value &&
		view.Config.APIKey.Configured &&
		(view.State == integration.StateConnected || view.State == integration.StateCouldNotVerify) {
		h.posterWall.Refresh()
	}
	if view.Effects.Reschedule && h.tmdbScheduler != nil {
		if err := h.tmdbScheduler.Reconfigure(); err != nil {
			h.reqLog(c).Error().Err(err).Msg("reconfiguring TMDB refresh schedule failed")
		}
	}
	if !view.Effects.RefreshStale || h.tmdbRuns == nil {
		return
	}
	initiatedBy := actorMemberID(c)
	result, err := h.tmdbRuns.Start(c.UserContext(), tmdbRunStart{
		Operation:   integration.RunOperationRefreshStale,
		Trigger:     integration.RunTriggerConfiguration,
		InitiatedBy: &initiatedBy,
	})
	if errors.Is(err, ErrTMDBLibraryRunActive) {
		return
	}
	if err != nil {
		h.reqLog(c).Error().Err(err).Msg("starting TMDB configuration refresh failed")
		return
	}
	if result.NoWork && h.integrationConfigs != nil {
		if err := h.integrationConfigs.UpdateLastChecked(c.UserContext(), "tmdb", result.CheckedAt); err != nil {
			h.reqLog(c).Error().Err(err).Msg("updating TMDB last checked time failed")
		}
	}
}

func (h *handler) handleTestTMDBConnection(c *fiber.Ctx) error {
	if ok, err := h.requireAdmin(c); !ok {
		return err
	}
	request, issues, err := parseTMDBDraft(c)
	if err != nil {
		return writeProblem(c, fiber.StatusBadRequest, "invalid_request", "invalid request body")
	}
	if len(issues) > 0 {
		return writeTMDBProblem(c, fiber.StatusUnprocessableEntity, "validation_failed", "TMDB settings are invalid", issues, nil)
	}
	result, err := h.tmdbIntegration.TestConnection(c.UserContext(), request)
	if err != nil {
		return h.writeTMDBIntegrationError(c, err, "testing TMDB connection failed")
	}
	h.applyTMDBConnectionResult(c, result)
	return c.Status(fiber.StatusOK).JSON(connectionResultResponse{
		State:     string(result.State),
		Reason:    result.Reason,
		CheckedAt: result.CheckedAt.UTC().Format(time.RFC3339),
	})
}

func (h *handler) applyTMDBConnectionResult(c *fiber.Ctx, result tmdb.ConnectionResult) {
	if h.tmdbScheduler == nil {
		return
	}
	var err error
	switch {
	case result.RuntimeResumed:
		err = h.tmdbScheduler.Reconfigure()
	case result.State == integration.StateError && result.Reason == "API key rejected" && result.RuntimeRevision > 0:
		err = h.tmdbScheduler.AuthenticationRejected(result.RuntimeRevision)
	}
	if err != nil {
		h.reqLog(c).Error().Err(err).Msg("updating TMDB refresh schedule after connection test failed")
	}
}

func parseTMDBDraft(c *fiber.Ctx) (tmdb.SaveDraft, []tmdb.ValidationIssue, error) {
	var request tmdbDraftRequest
	if err := c.BodyParser(&request); err != nil {
		return tmdb.SaveDraft{}, nil, err
	}
	settings := request.Settings
	admin := tmdb.AdminConfig{
		Enabled:    settings.Enabled,
		CastLimit:  settings.CastLimit,
		MaxRetries: settings.MaxRetries,
		BatchLimit: settings.BatchLimit,
	}
	issues := make([]tmdb.ValidationIssue, 0)
	admin.RefreshInterval = durationRequest("refreshInterval", settings.RefreshIntervalMS, &issues)
	admin.TTL = durationRequest("ttl", settings.TTLMS, &issues)
	admin.MinInterval = durationRequest("minInterval", settings.MinIntervalMS, &issues)
	admin.Backoff = durationRequest("backoff", settings.BackoffMS, &issues)
	return tmdb.SaveDraft{
		Revision:        request.Revision,
		Admin:           admin,
		RemoveFallbacks: request.RemoveFallbacks,
		APIKey:          request.APIKey,
		ClearAPIKey:     request.ClearAPIKey,
		ConfirmWarnings: request.ConfirmWarnings,
	}, issues, nil
}

func durationRequest(field string, milliseconds *int64, issues *[]tmdb.ValidationIssue) *time.Duration {
	if milliseconds == nil {
		return nil
	}
	if *milliseconds > math.MaxInt64/int64(time.Millisecond) || *milliseconds < math.MinInt64/int64(time.Millisecond) {
		*issues = append(*issues, tmdb.ValidationIssue{Field: field, Message: "is out of range"})
		return nil
	}
	value := time.Duration(*milliseconds) * time.Millisecond
	return &value
}

func (h *handler) writeTMDBIntegrationError(c *fiber.Ctx, err error, logMessage string) error {
	var validation *tmdb.ValidationError
	var warning *tmdb.WarningConfirmationError
	var authentication *tmdb.AuthenticationError
	switch {
	case errors.As(err, &validation):
		return writeTMDBProblem(c, fiber.StatusUnprocessableEntity, "validation_failed", validation.Error(), validation.Issues, nil)
	case errors.As(err, &warning):
		return writeTMDBProblem(c, fiber.StatusConflict, "confirmation_required", warning.Error(), nil, warning.Warnings)
	case errors.As(err, &authentication):
		return writeProblem(c, fiber.StatusUnprocessableEntity, "authentication_failed", authentication.Error())
	case errors.Is(err, integration.ErrStaleRevision):
		return writeProblem(c, fiber.StatusConflict, "stale_revision", "another admin changed these settings")
	case errors.Is(err, integration.ErrCredentialUnavailable):
		return writeProblem(c, fiber.StatusConflict, "credential_unavailable", "the stored TMDB credential is unavailable")
	default:
		return h.writeInternal(c, err, logMessage)
	}
}

func writeTMDBProblem(
	c *fiber.Ctx,
	status int,
	title string,
	detail string,
	issues []tmdb.ValidationIssue,
	warnings []tmdb.ConfigWarning,
) error {
	c.Set(fiber.HeaderContentType, "application/problem+json")
	return c.Status(status).JSON(integrationProblemDetails{
		Type: "about:blank", Title: title, Status: status, Detail: detail,
		Issues:   issues,
		Warnings: warnings,
	})
}

func toTMDBIntegrationResponse(view tmdb.ConfigView) tmdbIntegrationResponse {
	config := view.Config
	return tmdbIntegrationResponse{
		Revision:               view.Revision,
		State:                  string(view.State),
		Reason:                 view.Reason,
		Warnings:               tmdb.EffectiveConfigWarnings(config),
		LastCheckedAt:          formatTime(view.LastCheckedAt),
		LastConnectionTestedAt: formatTime(view.LastConnectionTestedAt),
		NextCheckAt:            formatTime(view.NextCheckAt),
		LastSuccessfulRunAt:    formatTime(view.LastSuccessfulRunAt),
		Settings: tmdbSettingsResponse{
			Enabled:         boolSetting(config.Enabled, "TMDB_ENABLED"),
			APIKey:          secretSetting(config.APIKey, "TMDB_API_KEY"),
			CastLimit:       intSetting(config.CastLimit, "TMDB_ENRICH_CAST_LIMIT"),
			RefreshInterval: durationSetting(config.RefreshInterval, "TMDB_ENRICH_REFRESH_INTERVAL"),
			TTL:             durationSetting(config.TTL, "TMDB_ENRICH_TTL"),
			MinInterval:     durationSetting(config.MinInterval, "TMDB_ENRICH_MIN_INTERVAL_MS"),
			MaxRetries:      intSetting(config.MaxRetries, "TMDB_ENRICH_MAX_RETRIES"),
			Backoff:         durationSetting(config.Backoff, "TMDB_ENRICH_BACKOFF_MS"),
			BatchLimit:      intSetting(config.BatchLimit, "TMDB_ENRICH_BATCH_LIMIT"),
		},
	}
}

func boolSetting(field integration.Field[bool], environment string) settingResponse[bool] {
	return settingResponse[bool]{
		Value: field.Value, Source: string(field.Source), Default: field.Default,
		HasAdminFallback: field.HasAdminFallback, Environment: environment,
	}
}

func intSetting(field integration.Field[int], environment string) settingResponse[int] {
	return settingResponse[int]{
		Value: field.Value, Source: string(field.Source), Default: field.Default,
		HasAdminFallback: field.HasAdminFallback, Environment: environment,
	}
}

func durationSetting(field integration.Field[time.Duration], environment string) settingResponse[int64] {
	return settingResponse[int64]{
		Value: field.Value.Milliseconds(), Source: string(field.Source), Default: field.Default.Milliseconds(),
		HasAdminFallback: field.HasAdminFallback, Environment: environment,
	}
}

func secretSetting(field integration.SecretField, environment string) secretSettingResponse {
	return secretSettingResponse{
		Configured: field.Configured, Source: string(field.Source),
		HasAdminFallback: field.HasAdminFallback, Environment: environment,
	}
}
