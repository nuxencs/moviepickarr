package radarr_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"moviepickarr/internal/integration/radarr"
)

func TestInteractiveSearchReturnsOpaqueSanitizedMatchedResultsApprovedFirst(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{"guid":"rejected-secret-guid","indexerId":4,"indexer":"Example","title":"Rejected.Release","mappedMovieId":8,"approved":false,"rejected":true,"rejections":["Quality is not wanted"],"downloadUrl":"https://secret.invalid/download","magnetUrl":"magnet:?xt=secret","infoHash":"secret-hash","size":200,"publishDate":"2026-08-06T19:00:00Z","protocol":"torrent","seeders":2,"leechers":3,"quality":{"quality":{"name":"HDTV-720p"}},"languages":[{"name":"English"}]},
			{"guid":"approved-secret-guid","indexerId":5,"indexer":"Example","title":"Approved.Release","mappedMovieId":8,"approved":true,"rejected":false,"size":100,"publishDate":"2026-08-07T19:00:00Z","protocol":"usenet","quality":{"quality":{"name":"Bluray-1080p"}},"customFormats":[{"name":"Good group"}],"customFormatScore":1200,"releaseGroup":"GROUP","edition":"Theatrical","languages":[{"name":"English"}]},
			{"guid":"wrong-movie-guid","indexerId":6,"title":"Wrong.Movie","mappedMovieId":9,"approved":true}
		]`))
	}))
	defer server.Close()

	client, err := radarr.NewHTTPClient(radarr.ClientConfig{BaseURL: server.URL, APIKey: "secret"})
	if err != nil {
		t.Fatalf("NewHTTPClient: %v", err)
	}
	releases, err := client.SearchReleases(context.Background(), 8)
	if err != nil {
		t.Fatalf("SearchReleases: %v", err)
	}
	if len(releases) != 2 {
		t.Fatalf("releases = %+v", releases)
	}
	if releases[0].Title != "Approved.Release" || !releases[0].Approved || releases[0].Rejected {
		t.Fatalf("first release = %+v", releases[0])
	}
	if len(releases[0].CustomFormats) != 1 || releases[0].CustomFormats[0] != "Good group" {
		t.Fatalf("first release custom formats = %+v", releases[0].CustomFormats)
	}
	if releases[1].Title != "Rejected.Release" || !releases[1].Rejected || len(releases[1].RejectionReasons) != 1 {
		t.Fatalf("second release = %+v", releases[1])
	}
	if releases[0].ID == "" || releases[1].ID == "" || releases[0].ID == releases[1].ID {
		t.Fatalf("opaque IDs = %q, %q", releases[0].ID, releases[1].ID)
	}
	encoded, _ := json.Marshal(releases)
	for _, forbidden := range []string{"secret-guid", "downloadUrl", "magnet:", "secret-hash", "guid", "infoHash"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("sanitized results contain %q: %s", forbidden, encoded)
		}
	}
}

func TestGrabReleaseResolvesOpaqueIDAndRequiresRejectedOverride(t *testing.T) {
	t.Parallel()

	var grabs []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(`[
				{"guid":"approved-guid","indexerId":5,"title":"Approved","mappedMovieId":8,"approved":true},
				{"guid":"rejected-guid","indexerId":6,"title":"Rejected","mappedMovieId":8,"approved":false,"rejected":true,"rejections":["Rejected by profile"],"quality":{"quality":{"id":7,"name":"Bluray-1080p","source":"bluray","resolution":1080},"revision":{"version":1,"real":0,"isRepack":false}},"languages":[{"id":1,"name":"English"}]}
			]`))
			return
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		grabs = append(grabs, body)
		_ = json.NewEncoder(w).Encode(body)
	}))
	defer server.Close()

	client, err := radarr.NewHTTPClient(radarr.ClientConfig{BaseURL: server.URL, APIKey: "secret"})
	if err != nil {
		t.Fatalf("NewHTTPClient: %v", err)
	}
	releases, err := client.SearchReleases(context.Background(), 8)
	if err != nil {
		t.Fatalf("SearchReleases: %v", err)
	}
	if err := client.GrabRelease(context.Background(), radarr.GrabReleaseRequest{ResultID: releases[1].ID}); !errors.Is(err, radarr.ErrRejectedRelease) {
		t.Fatalf("rejected grab error = %v", err)
	}
	if len(grabs) != 0 {
		t.Fatalf("rejected release was sent without confirmation: %+v", grabs)
	}
	if err := client.GrabRelease(context.Background(), radarr.GrabReleaseRequest{ResultID: releases[1].ID, AllowRejected: true}); err != nil {
		t.Fatalf("confirmed rejected grab: %v", err)
	}
	if len(grabs) != 1 || grabs[0]["guid"] != "rejected-guid" || grabs[0]["indexerId"] != float64(6) {
		t.Fatalf("grab body = %+v", grabs)
	}
	if grabs[0]["shouldOverride"] != true || grabs[0]["movieId"] != float64(8) || grabs[0]["quality"] == nil || grabs[0]["languages"] == nil {
		t.Fatalf("rejected override body = %+v", grabs[0])
	}
	if _, exposed := grabs[0]["resultId"]; exposed {
		t.Fatalf("opaque result ID was sent to Radarr: %+v", grabs[0])
	}
}

func TestGrabApprovedReleaseSendsOnlyRadarrIdentifiers(t *testing.T) {
	t.Parallel()

	var grab map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(`[{"guid":"approved-guid","indexerId":5,"title":"Approved","mappedMovieId":8,"approved":true}]`))
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&grab)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client, err := radarr.NewHTTPClient(radarr.ClientConfig{BaseURL: server.URL, APIKey: "secret"})
	if err != nil {
		t.Fatalf("NewHTTPClient: %v", err)
	}
	releases, err := client.SearchReleases(context.Background(), 8)
	if err != nil {
		t.Fatalf("SearchReleases: %v", err)
	}
	if err := client.GrabRelease(context.Background(), radarr.GrabReleaseRequest{ResultID: releases[0].ID}); err != nil {
		t.Fatalf("GrabRelease: %v", err)
	}
	if len(grab) != 2 || grab["guid"] != "approved-guid" || grab["indexerId"] != float64(5) {
		t.Fatalf("approved grab body = %+v", grab)
	}
}

func TestRejectedOverrideSerializesRequiredFieldsWhenLanguagesAreEmpty(t *testing.T) {
	t.Parallel()

	var grab map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(`[{"guid":"rejected-guid","indexerId":5,"title":"Rejected","mappedMovieId":8,"approved":false,"rejected":true}]`))
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&grab)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client, err := radarr.NewHTTPClient(radarr.ClientConfig{BaseURL: server.URL, APIKey: "secret"})
	if err != nil {
		t.Fatalf("NewHTTPClient: %v", err)
	}
	releases, err := client.SearchReleases(context.Background(), 8)
	if err != nil {
		t.Fatalf("SearchReleases: %v", err)
	}
	err = client.GrabRelease(context.Background(), radarr.GrabReleaseRequest{ResultID: releases[0].ID, AllowRejected: true})
	if err != nil {
		t.Fatalf("GrabRelease: %v", err)
	}
	if grab["quality"] == nil || grab["languages"] == nil {
		t.Fatalf("required override fields = %+v", grab)
	}
}

func TestGrabReleaseMapsRemoteNotFoundToExpiredResult(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(`[{"guid":"guid","indexerId":5,"title":"Release","mappedMovieId":8,"approved":true}]`))
			return
		}
		http.Error(w, `{"message":"not found"}`, http.StatusNotFound)
	}))
	defer server.Close()

	client, err := radarr.NewHTTPClient(radarr.ClientConfig{BaseURL: server.URL, APIKey: "secret"})
	if err != nil {
		t.Fatalf("NewHTTPClient: %v", err)
	}
	releases, err := client.SearchReleases(context.Background(), 8)
	if err != nil {
		t.Fatalf("SearchReleases: %v", err)
	}
	err = client.GrabRelease(context.Background(), radarr.GrabReleaseRequest{ResultID: releases[0].ID})
	if !errors.Is(err, radarr.ErrReleaseExpired) {
		t.Fatalf("remote not-found error = %v", err)
	}
	if err := client.GrabRelease(context.Background(), radarr.GrabReleaseRequest{ResultID: releases[0].ID}); !errors.Is(err, radarr.ErrReleaseExpired) {
		t.Fatalf("invalidated result error = %v", err)
	}
}

func TestGrabReleaseRejectsExpiredOpaqueResult(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"guid":"guid","indexerId":5,"title":"Release","mappedMovieId":8,"approved":true}]`))
	}))
	defer server.Close()

	client, err := radarr.NewHTTPClient(radarr.ClientConfig{
		BaseURL: server.URL, APIKey: "secret", ReleaseCacheTTL: time.Nanosecond,
	})
	if err != nil {
		t.Fatalf("NewHTTPClient: %v", err)
	}
	releases, err := client.SearchReleases(context.Background(), 8)
	if err != nil {
		t.Fatalf("SearchReleases: %v", err)
	}
	if err := client.GrabRelease(context.Background(), radarr.GrabReleaseRequest{ResultID: releases[0].ID}); !errors.Is(err, radarr.ErrReleaseExpired) {
		t.Fatalf("expired grab error = %v", err)
	}
}
