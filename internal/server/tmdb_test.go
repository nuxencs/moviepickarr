package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func testClient(srvURL string) *tmdbClient {
	return &tmdbClient{
		apiKey:      "test-key",
		baseURL:     srvURL,
		http:        &http.Client{Timeout: 2 * time.Second},
		maxRetries:  1,
		backoffBase: time.Millisecond,
		// no limiter: tests should not be paced
	}
}

func TestFindByIMDb_Success(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("missing/incorrect auth header: %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"movie_results":[{"id":603,"title":"The Matrix"}]}`))
	}))
	defer srv.Close()

	got, err := testClient(srv.URL).FindByIMDb(context.Background(), "tt0133093")
	if err != nil {
		t.Fatalf("FindByIMDb: %v", err)
	}
	if got.ID != 603 {
		t.Fatalf("expected id 603, got %d", got.ID)
	}
}

func TestFindByIMDb_EmptyResultsIsNotFound(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"movie_results":[]}`))
	}))
	defer srv.Close()

	_, err := testClient(srv.URL).FindByIMDb(context.Background(), "tt0000000")
	if !errors.Is(err, errTMDBNotFound) {
		t.Fatalf("expected errTMDBNotFound, got %v", err)
	}
}

func TestMovieDetails_Decode(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Credits must arrive in the same single API call.
		if got := r.URL.Query().Get("append_to_response"); got != "credits" {
			t.Errorf("expected append_to_response=credits, got %q (url %s)", got, r.URL)
		}
		_, _ = w.Write([]byte(`{
			"id":603,"imdb_id":"tt0133093","overview":"ov",
			"poster_path":null,"backdrop_path":"/bd.jpg",
			"release_date":"1999-03-30","runtime":136,
			"genres":[{"id":28,"name":"Action"},{"id":878,"name":"Science Fiction"}],
			"vote_average":8.2,"vote_count":100,"tagline":"tg",
			"credits":{
				"cast":[{"id":6384,"name":"Keanu Reeves","profile_path":"/kr.jpg","character":"Neo","order":0}],
				"crew":[{"id":9340,"name":"Lana Wachowski","profile_path":null,"job":"Director","department":"Directing"}]
			}
		}`))
	}))
	defer srv.Close()

	got, err := testClient(srv.URL).MovieDetails(context.Background(), 603)
	if err != nil {
		t.Fatalf("MovieDetails: %v", err)
	}
	if got.PosterPath != nil {
		t.Fatalf("expected null poster -> nil, got %v", *got.PosterPath)
	}
	if got.Runtime == nil || *got.Runtime != 136 {
		t.Fatalf("runtime mismatch: %v", got.Runtime)
	}
	if len(got.Genres) != 2 || got.Genres[1].Name != "Science Fiction" {
		t.Fatalf("genres mismatch: %v", got.Genres)
	}

	if len(got.Credits.Cast) != 1 {
		t.Fatalf("expected 1 cast member, got %d", len(got.Credits.Cast))
	}
	cast := got.Credits.Cast[0]
	if cast.ID != 6384 || cast.Name != "Keanu Reeves" || cast.Character != "Neo" || cast.Order != 0 {
		t.Fatalf("cast mismatch: %+v", cast)
	}
	if cast.ProfilePath == nil || *cast.ProfilePath != "/kr.jpg" {
		t.Fatalf("cast profile mismatch: %v", cast.ProfilePath)
	}
	if len(got.Credits.Crew) != 1 {
		t.Fatalf("expected 1 crew member, got %d", len(got.Credits.Crew))
	}
	crew := got.Credits.Crew[0]
	if crew.ID != 9340 || crew.Job != "Director" || crew.Department != "Directing" {
		t.Fatalf("crew mismatch: %+v", crew)
	}
	if crew.ProfilePath != nil {
		t.Fatalf("expected null crew profile -> nil, got %v", *crew.ProfilePath)
	}
}

func TestDoRequest_NotFound(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	_, err := testClient(srv.URL).MovieDetails(context.Background(), 1)
	if !errors.Is(err, errTMDBNotFound) {
		t.Fatalf("expected errTMDBNotFound, got %v", err)
	}
}

func TestDoRequest_RateLimitedExhausts(t *testing.T) {
	t.Parallel()
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.Header().Set("Retry-After", "0")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	_, err := testClient(srv.URL).MovieDetails(context.Background(), 1)
	var rl *tmdbRateLimitError
	if !errors.As(err, &rl) {
		t.Fatalf("expected *tmdbRateLimitError, got %v", err)
	}
	if calls != 2 { // initial + 1 retry (maxRetries=1)
		t.Fatalf("expected 2 calls, got %d", calls)
	}
}

func TestDoRequest_ServerErrorIsTransient(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	_, err := testClient(srv.URL).MovieDetails(context.Background(), 1)
	var tr *tmdbTransientError
	if !errors.As(err, &tr) {
		t.Fatalf("expected *tmdbTransientError, got %v", err)
	}
}

func TestDoRequest_OtherClientErrorIsPermanent(t *testing.T) {
	t.Parallel()
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	_, err := testClient(srv.URL).MovieDetails(context.Background(), 1)
	if err == nil || errors.Is(err, errTMDBNotFound) {
		t.Fatalf("expected a permanent non-notfound error, got %v", err)
	}
	var tr *tmdbTransientError
	var rl *tmdbRateLimitError
	if errors.As(err, &tr) || errors.As(err, &rl) {
		t.Fatalf("401 should not be retried/transient, got %v", err)
	}
	if calls != 1 {
		t.Fatalf("expected exactly 1 call (no retry on 401), got %d", calls)
	}
}

func TestDoRequest_NotConfigured(t *testing.T) {
	t.Parallel()
	c := &tmdbClient{apiKey: "", baseURL: "http://unused", http: &http.Client{}}
	_, err := c.MovieDetails(context.Background(), 1)
	if !errors.Is(err, errTMDBNotConfigured) {
		t.Fatalf("expected errTMDBNotConfigured, got %v", err)
	}
}

func TestParseRetryAfter(t *testing.T) {
	t.Parallel()
	if d := parseRetryAfter("5", time.Second); d != 5*time.Second {
		t.Fatalf("delta-seconds: expected 5s, got %s", d)
	}
	if d := parseRetryAfter("", 3*time.Second); d != 3*time.Second {
		t.Fatalf("empty: expected fallback 3s, got %s", d)
	}
	if d := parseRetryAfter("garbage", 2*time.Second); d != 2*time.Second {
		t.Fatalf("garbage: expected fallback 2s, got %s", d)
	}
}
