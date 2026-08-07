package tmdb

import (
	"testing"
	"time"

	"moviepickarr/internal/integration"
)

func TestResolveConfig_EnvironmentWinsAndRetainsAdminFallback(t *testing.T) {
	resolved := ResolveConfig(
		EnvironmentConfig{
			CastLimit: new(24),
		},
		AdminConfig{
			CastLimit: new(30),
		},
		false,
	)

	if got := resolved.CastLimit.Value; got != 24 {
		t.Fatalf("cast limit = %d, want 24", got)
	}
	if got := resolved.CastLimit.Source; got != integration.SourceEnvironment {
		t.Fatalf("source = %q, want %q", got, integration.SourceEnvironment)
	}
	if !resolved.CastLimit.HasAdminFallback {
		t.Fatal("expected dormant admin fallback")
	}
	if got := resolved.CastLimit.Default; got != 15 {
		t.Fatalf("default = %d, want 15", got)
	}
	if got := resolved.RefreshInterval.Value; got != time.Hour {
		t.Fatalf("refresh interval = %s, want 1h", got)
	}
}

func TestResolveConfig_APIKeyIsWriteOnlyAndEnablesTMDBByDefault(t *testing.T) {
	resolved := ResolveConfig(
		EnvironmentConfig{APIKey: "environment-secret"},
		AdminConfig{},
		true,
	)

	if !resolved.APIKey.Configured {
		t.Fatal("expected API key to be configured")
	}
	if got := resolved.APIKey.Source; got != integration.SourceEnvironment {
		t.Fatalf("API key source = %q, want %q", got, integration.SourceEnvironment)
	}
	if !resolved.APIKey.HasAdminFallback {
		t.Fatal("expected dormant Admin API key")
	}
	if !resolved.Enabled.Value || resolved.Enabled.Source != integration.SourceDefault {
		t.Fatalf("enabled = %+v, want derived enabled default", resolved.Enabled)
	}
	if resolved.Enabled.Default != true {
		t.Fatalf("enabled default = %t, want true", resolved.Enabled.Default)
	}
}

func TestResolveConfig_AdminValuesOverrideBuiltInDefaults(t *testing.T) {
	resolved := ResolveConfig(EnvironmentConfig{}, AdminConfig{
		Enabled:         new(false),
		CastLimit:       new(0),
		RefreshInterval: new(time.Duration(0)),
		TTL:             new(48 * time.Hour),
		MinInterval:     new(400 * time.Millisecond),
		MaxRetries:      new(2),
		Backoff:         new(750 * time.Millisecond),
		BatchLimit:      new(75),
	}, true)

	if resolved.Enabled.Value || resolved.Enabled.Source != integration.SourceAdmin {
		t.Fatalf("enabled = %+v, want disabled Admin value", resolved.Enabled)
	}
	if resolved.CastLimit.Value != 0 || resolved.RefreshInterval.Value != 0 {
		t.Fatalf("special zero values were not retained: %+v", resolved)
	}
	if resolved.TTL.Value != 48*time.Hour || resolved.MinInterval.Value != 400*time.Millisecond {
		t.Fatalf("duration values = ttl %s interval %s", resolved.TTL.Value, resolved.MinInterval.Value)
	}
	if resolved.MaxRetries.Value != 2 || resolved.Backoff.Value != 750*time.Millisecond || resolved.BatchLimit.Value != 75 {
		t.Fatalf("advanced values = %+v", resolved)
	}
	if resolved.TTL.Default != 30*24*time.Hour || resolved.MaxRetries.Default != 4 || resolved.BatchLimit.Default != 200 {
		t.Fatalf("unexpected defaults: %+v", resolved)
	}
}

func TestValidateAdminConfig_ReportsEveryInvalidField(t *testing.T) {
	issues := ValidateAdminConfig(AdminConfig{
		CastLimit:       new(-1),
		RefreshInterval: new(-time.Second),
		TTL:             new(time.Duration(0)),
		MinInterval:     new(-time.Millisecond),
		MaxRetries:      new(-1),
		Backoff:         new(-time.Millisecond),
		BatchLimit:      new(0),
	})

	want := []string{
		"castLimit",
		"refreshInterval",
		"ttl",
		"minInterval",
		"maxRetries",
		"backoff",
		"batchLimit",
	}
	if len(issues) != len(want) {
		t.Fatalf("issues = %+v, want fields %v", issues, want)
	}
	for i, field := range want {
		if issues[i].Field != field {
			t.Fatalf("issue %d field = %q, want %q", i, issues[i].Field, field)
		}
	}
}

func TestConfigWarnings_FlagAggressiveValues(t *testing.T) {
	warnings := ConfigWarnings(AdminConfig{
		RefreshInterval: new(14 * time.Minute),
		TTL:             new(59 * time.Minute),
		MinInterval:     new(249 * time.Millisecond),
	})

	want := []string{"refreshInterval", "ttl", "minInterval"}
	if len(warnings) != len(want) {
		t.Fatalf("warnings = %+v, want fields %v", warnings, want)
	}
	for i, field := range want {
		if warnings[i].Field != field {
			t.Fatalf("warning %d field = %q, want %q", i, warnings[i].Field, field)
		}
	}
}

func TestEffectiveConfigWarnings_IncludesActiveEnvironmentValues(t *testing.T) {
	resolved := ResolveConfig(EnvironmentConfig{
		RefreshInterval: new(10 * time.Minute),
		TTL:             new(30 * time.Minute),
		MinInterval:     new(100 * time.Millisecond),
	}, AdminConfig{}, false)

	warnings := EffectiveConfigWarnings(resolved)
	if len(warnings) != 3 {
		t.Fatalf("warnings = %+v, want all aggressive environment values", warnings)
	}
	for _, warning := range warnings {
		if warning.Source != integration.SourceEnvironment {
			t.Fatalf("warning = %+v, want environment source", warning)
		}
	}
}
