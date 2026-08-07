package radarr_test

import (
	"testing"

	"moviepickarr/internal/integration/radarr"
)

func TestExactIdentityRepresentsOneValidatedProviderID(t *testing.T) {
	t.Parallel()

	tmdb, err := radarr.TMDBIdentity(27205)
	if err != nil {
		t.Fatalf("TMDBIdentity: %v", err)
	}
	if tmdb.Kind != radarr.IdentityTMDB || tmdb.TMDBID != 27205 || tmdb.IMDbID != "" {
		t.Fatalf("TMDB identity = %+v", tmdb)
	}

	imdb, err := radarr.IMDbIdentity(" TT1375666 ")
	if err != nil {
		t.Fatalf("IMDbIdentity: %v", err)
	}
	if imdb.Kind != radarr.IdentityIMDb || imdb.IMDbID != "tt1375666" || imdb.TMDBID != 0 {
		t.Fatalf("IMDb identity = %+v", imdb)
	}
}

func TestExactIdentityRejectsInvalidProviderIDs(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		make func() error
	}{
		{name: "zero TMDB", make: func() error { _, err := radarr.TMDBIdentity(0); return err }},
		{name: "short IMDb", make: func() error { _, err := radarr.IMDbIdentity("tt123"); return err }},
		{name: "IMDb letters", make: func() error { _, err := radarr.IMDbIdentity("tt123456x"); return err }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if err := tc.make(); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestMinimumAvailabilityUsesRadarrWireValues(t *testing.T) {
	t.Parallel()

	got := []radarr.MinimumAvailability{
		radarr.AvailabilityTBA,
		radarr.AvailabilityAnnounced,
		radarr.AvailabilityInCinemas,
		radarr.AvailabilityReleased,
	}
	want := []radarr.MinimumAvailability{"tba", "announced", "inCinemas", "released"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("availability %d = %q, want %q", i, got[i], want[i])
		}
	}
}
