package integration_test

import (
	"testing"

	"moviepickarr/internal/integration"
)

func TestResolveFieldAppliesEnvironmentAdminDefaultPrecedence(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name        string
		environment *int
		admin       *int
		want        integration.Field[int]
	}{
		{
			name:        "environment with dormant admin fallback",
			environment: new(24),
			admin:       new(30),
			want: integration.Field[int]{
				Value: 24, Source: integration.SourceEnvironment, Default: 15, HasAdminFallback: true,
			},
		},
		{
			name:        "environment without fallback",
			environment: new(24),
			want: integration.Field[int]{
				Value: 24, Source: integration.SourceEnvironment, Default: 15,
			},
		},
		{
			name:  "admin",
			admin: new(30),
			want: integration.Field[int]{
				Value: 30, Source: integration.SourceAdmin, Default: 15,
			},
		},
		{
			name: "default",
			want: integration.Field[int]{
				Value: 15, Source: integration.SourceDefault, Default: 15,
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := integration.ResolveField(test.environment, test.admin, 15); got != test.want {
				t.Fatalf("resolved field = %+v, want %+v", got, test.want)
			}
		})
	}
}

func TestResolveSecretExposesMetadataWithoutSecretValues(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name        string
		environment bool
		admin       bool
		want        integration.SecretField
	}{
		{
			name:        "environment with dormant admin fallback",
			environment: true,
			admin:       true,
			want: integration.SecretField{
				Configured: true, Source: integration.SourceEnvironment, HasAdminFallback: true,
			},
		},
		{
			name:        "environment without fallback",
			environment: true,
			want: integration.SecretField{
				Configured: true, Source: integration.SourceEnvironment,
			},
		},
		{
			name:  "admin",
			admin: true,
			want: integration.SecretField{
				Configured: true, Source: integration.SourceAdmin,
			},
		},
		{
			name: "not configured",
			want: integration.SecretField{Source: integration.SourceDefault},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := integration.ResolveSecretField(test.environment, test.admin); got != test.want {
				t.Fatalf("resolved secret = %+v, want %+v", got, test.want)
			}
		})
	}
}
