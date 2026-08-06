package tmdb

import (
	"math"
	"strconv"
	"strings"
	"time"
)

type EnvironmentIssue struct {
	Field   string
	Message string
}

type EnvironmentLookup func(string) (string, bool)

func LoadEnvironmentConfig(lookup EnvironmentLookup) (EnvironmentConfig, []EnvironmentIssue) {
	var config EnvironmentConfig
	issues := make([]EnvironmentIssue, 0)

	if raw, ok := lookup("TMDB_API_KEY"); ok {
		config.APIKey = strings.TrimSpace(raw)
	}
	config.Enabled = parseEnvironmentBool(lookup, "TMDB_ENABLED", &issues)
	config.CastLimit = parseEnvironmentInt(lookup, "TMDB_ENRICH_CAST_LIMIT", true, &issues)
	config.RefreshInterval = parseEnvironmentDuration(lookup, "TMDB_ENRICH_REFRESH_INTERVAL", true, &issues)
	config.TTL = parseEnvironmentDuration(lookup, "TMDB_ENRICH_TTL", false, &issues)
	config.MinInterval = parseEnvironmentMilliseconds(lookup, "TMDB_ENRICH_MIN_INTERVAL_MS", true, &issues)
	config.MaxRetries = parseEnvironmentInt(lookup, "TMDB_ENRICH_MAX_RETRIES", true, &issues)
	config.Backoff = parseEnvironmentMilliseconds(lookup, "TMDB_ENRICH_BACKOFF_MS", true, &issues)
	config.BatchLimit = parseEnvironmentInt(lookup, "TMDB_ENRICH_BATCH_LIMIT", false, &issues)
	return config, issues
}

func parseEnvironmentBool(lookup EnvironmentLookup, key string, issues *[]EnvironmentIssue) *bool {
	raw, ok := environmentValue(lookup, key)
	if !ok {
		return nil
	}
	value, err := strconv.ParseBool(raw)
	if err != nil {
		*issues = append(*issues, EnvironmentIssue{Field: key, Message: "must be a boolean"})
		return nil
	}
	return &value
}

func parseEnvironmentInt(lookup EnvironmentLookup, key string, zeroAllowed bool, issues *[]EnvironmentIssue) *int {
	raw, ok := environmentValue(lookup, key)
	if !ok {
		return nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 0 || (!zeroAllowed && value == 0) {
		*issues = append(*issues, EnvironmentIssue{Field: key, Message: "must be a valid non-negative integer"})
		return nil
	}
	return &value
}

func parseEnvironmentDuration(lookup EnvironmentLookup, key string, zeroAllowed bool, issues *[]EnvironmentIssue) *time.Duration {
	raw, ok := environmentValue(lookup, key)
	if !ok {
		return nil
	}
	value, err := time.ParseDuration(raw)
	if err != nil || value < 0 || (!zeroAllowed && value == 0) {
		*issues = append(*issues, EnvironmentIssue{Field: key, Message: "must be a valid non-negative duration"})
		return nil
	}
	return &value
}

func parseEnvironmentMilliseconds(lookup EnvironmentLookup, key string, zeroAllowed bool, issues *[]EnvironmentIssue) *time.Duration {
	value := parseEnvironmentInt(lookup, key, zeroAllowed, issues)
	if value == nil {
		return nil
	}
	if int64(*value) > math.MaxInt64/int64(time.Millisecond) {
		*issues = append(*issues, EnvironmentIssue{Field: key, Message: "is too large"})
		return nil
	}
	duration := time.Duration(*value) * time.Millisecond
	return &duration
}

func environmentValue(lookup EnvironmentLookup, key string) (string, bool) {
	raw, ok := lookup(key)
	if !ok || strings.TrimSpace(raw) == "" {
		return "", false
	}
	return strings.TrimSpace(raw), true
}
