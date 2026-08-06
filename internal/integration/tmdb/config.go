package tmdb

import (
	"time"

	"moviepickarr/internal/integration"
)

type EnvironmentConfig struct {
	Enabled         *bool
	APIKey          string
	CastLimit       *int
	RefreshInterval *time.Duration
	TTL             *time.Duration
	MinInterval     *time.Duration
	MaxRetries      *int
	Backoff         *time.Duration
	BatchLimit      *int
}

type AdminConfig struct {
	Enabled         *bool          `json:"enabled,omitempty"`
	CastLimit       *int           `json:"castLimit,omitempty"`
	RefreshInterval *time.Duration `json:"refreshInterval,omitempty"`
	TTL             *time.Duration `json:"ttl,omitempty"`
	MinInterval     *time.Duration `json:"minInterval,omitempty"`
	MaxRetries      *int           `json:"maxRetries,omitempty"`
	Backoff         *time.Duration `json:"backoff,omitempty"`
	BatchLimit      *int           `json:"batchLimit,omitempty"`
}

type ResolvedConfig struct {
	Enabled         integration.Field[bool]
	APIKey          integration.SecretField
	CastLimit       integration.Field[int]
	RefreshInterval integration.Field[time.Duration]
	TTL             integration.Field[time.Duration]
	MinInterval     integration.Field[time.Duration]
	MaxRetries      integration.Field[int]
	Backoff         integration.Field[time.Duration]
	BatchLimit      integration.Field[int]
}

type ValidationIssue struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

func ValidateAdminConfig(config AdminConfig) []ValidationIssue {
	issues := make([]ValidationIssue, 0)
	if config.CastLimit != nil && *config.CastLimit < 0 {
		issues = append(issues, ValidationIssue{Field: "castLimit", Message: "must be zero or greater"})
	}
	if config.RefreshInterval != nil && *config.RefreshInterval < 0 {
		issues = append(issues, ValidationIssue{Field: "refreshInterval", Message: "must be zero or greater"})
	}
	if config.TTL != nil && *config.TTL <= 0 {
		issues = append(issues, ValidationIssue{Field: "ttl", Message: "must be greater than zero"})
	}
	if config.MinInterval != nil && *config.MinInterval < 0 {
		issues = append(issues, ValidationIssue{Field: "minInterval", Message: "must be zero or greater"})
	}
	if config.MaxRetries != nil && *config.MaxRetries < 0 {
		issues = append(issues, ValidationIssue{Field: "maxRetries", Message: "must be zero or greater"})
	}
	if config.Backoff != nil && *config.Backoff < 0 {
		issues = append(issues, ValidationIssue{Field: "backoff", Message: "must be zero or greater"})
	}
	if config.BatchLimit != nil && *config.BatchLimit <= 0 {
		issues = append(issues, ValidationIssue{Field: "batchLimit", Message: "must be greater than zero"})
	}
	return issues
}

type ConfigWarning struct {
	Field   string             `json:"field"`
	Message string             `json:"message"`
	Source  integration.Source `json:"source,omitempty"`
}

func ConfigWarnings(config AdminConfig) []ConfigWarning {
	warnings := make([]ConfigWarning, 0, 3)
	if config.RefreshInterval != nil && *config.RefreshInterval > 0 && *config.RefreshInterval < 15*time.Minute {
		warnings = append(warnings, ConfigWarning{
			Field:   "refreshInterval",
			Message: "refresh intervals below 15 minutes may create heavy TMDB traffic",
		})
	}
	if config.TTL != nil && *config.TTL < time.Hour {
		warnings = append(warnings, ConfigWarning{
			Field:   "ttl",
			Message: "metadata freshness below 1 hour may create heavy TMDB traffic",
		})
	}
	if config.MinInterval != nil && *config.MinInterval < 250*time.Millisecond {
		warnings = append(warnings, ConfigWarning{
			Field:   "minInterval",
			Message: "request intervals below 250 ms may exceed TMDB limits",
		})
	}
	return warnings
}

func EffectiveConfigWarnings(config ResolvedConfig) []ConfigWarning {
	active := AdminConfig{
		RefreshInterval: &config.RefreshInterval.Value,
		TTL:             &config.TTL.Value,
		MinInterval:     &config.MinInterval.Value,
	}
	warnings := ConfigWarnings(active)
	for i := range warnings {
		switch warnings[i].Field {
		case "refreshInterval":
			warnings[i].Source = config.RefreshInterval.Source
		case "ttl":
			warnings[i].Source = config.TTL.Source
		case "minInterval":
			warnings[i].Source = config.MinInterval.Source
		}
	}
	return warnings
}

func ResolveConfig(env EnvironmentConfig, admin AdminConfig, hasAdminAPIKey bool) ResolvedConfig {
	hasEnvironmentAPIKey := env.APIKey != ""
	apiKey := integration.ResolveSecretField(hasEnvironmentAPIKey, hasAdminAPIKey)

	enabledDefault := apiKey.Configured
	return ResolvedConfig{
		Enabled:         integration.ResolveField(env.Enabled, admin.Enabled, enabledDefault),
		APIKey:          apiKey,
		CastLimit:       integration.ResolveField(env.CastLimit, admin.CastLimit, 15),
		RefreshInterval: integration.ResolveField(env.RefreshInterval, admin.RefreshInterval, time.Hour),
		TTL:             integration.ResolveField(env.TTL, admin.TTL, 30*24*time.Hour),
		MinInterval:     integration.ResolveField(env.MinInterval, admin.MinInterval, 250*time.Millisecond),
		MaxRetries:      integration.ResolveField(env.MaxRetries, admin.MaxRetries, 4),
		Backoff:         integration.ResolveField(env.Backoff, admin.Backoff, 500*time.Millisecond),
		BatchLimit:      integration.ResolveField(env.BatchLimit, admin.BatchLimit, 200),
	}
}
