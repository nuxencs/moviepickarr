package tmdb

import (
	"testing"
	"time"
)

func TestLoadEnvironmentConfig_ParsesTypedTMDBValues(t *testing.T) {
	values := map[string]string{
		"TMDB_ENABLED":                 "false",
		"TMDB_API_KEY":                 "environment-key",
		"TMDB_ENRICH_CAST_LIMIT":       "0",
		"TMDB_ENRICH_REFRESH_INTERVAL": "90m",
		"TMDB_ENRICH_TTL":              "48h",
		"TMDB_ENRICH_MIN_INTERVAL_MS":  "400",
		"TMDB_ENRICH_MAX_RETRIES":      "2",
		"TMDB_ENRICH_BACKOFF_MS":       "750",
		"TMDB_ENRICH_BATCH_LIMIT":      "75",
	}

	config, issues := LoadEnvironmentConfig(func(key string) (string, bool) {
		value, ok := values[key]
		return value, ok
	})

	if len(issues) != 0 {
		t.Fatalf("issues = %+v", issues)
	}
	if config.Enabled == nil || *config.Enabled {
		t.Fatalf("enabled = %v, want false", config.Enabled)
	}
	if config.APIKey != "environment-key" {
		t.Fatalf("API key was not loaded")
	}
	if config.CastLimit == nil || *config.CastLimit != 0 {
		t.Fatalf("cast limit = %v, want 0", config.CastLimit)
	}
	if config.RefreshInterval == nil || *config.RefreshInterval != 90*time.Minute {
		t.Fatalf("refresh interval = %v", config.RefreshInterval)
	}
	if config.TTL == nil || *config.TTL != 48*time.Hour {
		t.Fatalf("ttl = %v", config.TTL)
	}
	if config.MinInterval == nil || *config.MinInterval != 400*time.Millisecond {
		t.Fatalf("minimum interval = %v", config.MinInterval)
	}
	if config.MaxRetries == nil || *config.MaxRetries != 2 {
		t.Fatalf("retries = %v", config.MaxRetries)
	}
	if config.Backoff == nil || *config.Backoff != 750*time.Millisecond {
		t.Fatalf("backoff = %v", config.Backoff)
	}
	if config.BatchLimit == nil || *config.BatchLimit != 75 {
		t.Fatalf("batch limit = %v", config.BatchLimit)
	}
}

func TestLoadEnvironmentConfig_RejectsOverflowingMilliseconds(t *testing.T) {
	config, issues := LoadEnvironmentConfig(func(key string) (string, bool) {
		if key == "TMDB_ENRICH_MIN_INTERVAL_MS" {
			return "9223372036854775807", true
		}
		return "", false
	})

	if config.MinInterval != nil {
		t.Fatalf("minimum interval = %v, want fallback", config.MinInterval)
	}
	if len(issues) != 1 || issues[0].Field != "TMDB_ENRICH_MIN_INTERVAL_MS" {
		t.Fatalf("issues = %+v", issues)
	}
}
