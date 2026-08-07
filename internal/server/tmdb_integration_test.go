package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	integrationtmdb "moviepickarr/internal/integration/tmdb"
)

func TestTMDBConnectionTester_UsesDraftRuntimeWithoutPersistingIt(t *testing.T) {
	t.Parallel()
	var authorization string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/configuration" {
			t.Fatalf("path = %q, want /configuration", r.URL.Path)
		}
		authorization = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"images":{"base_url":"https://image.tmdb.org/t/p/"}}`))
	}))
	defer srv.Close()
	tester := newTMDBConnectionTester(srv.URL, srv.Client())

	err := tester.TestConnection(context.Background(), integrationtmdb.RuntimeConfig{
		APIKey:      "draft-key",
		MinInterval: 0,
		MaxRetries:  0,
		Backoff:     time.Millisecond,
	})
	if err != nil {
		t.Fatalf("test connection: %v", err)
	}
	if authorization != "Bearer draft-key" {
		t.Fatalf("authorization = %q", authorization)
	}
}

func TestTMDBConnectionTester_MapsRejectedCredentials(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()
	tester := newTMDBConnectionTester(srv.URL, srv.Client())

	err := tester.TestConnection(context.Background(), integrationtmdb.RuntimeConfig{
		APIKey:  "rejected-key",
		Backoff: time.Millisecond,
	})
	if !errors.Is(err, integrationtmdb.ErrAuthentication) {
		t.Fatalf("error = %v, want TMDB authentication error", err)
	}
}
