package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"moviepickarr/internal/integration"

	"github.com/gofiber/fiber/v2"
)

type errorTMDBSearcher struct {
	err error
}

func (s errorTMDBSearcher) Search(context.Context, string) ([]tmdbMovie, error) {
	return nil, s.err
}

func TestHandleTMDBSearch_UnavailableDoesNotExposeAdminConfiguration(t *testing.T) {
	t.Parallel()
	_, app, _, _ := setupEditMovieTest(t)

	response := doAs(t, app, httptest.NewRequest(http.MethodGet, "/api/v1/tmdb/search?query=Alien", nil), 1, "member")
	defer response.Body.Close()
	if response.StatusCode != fiber.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", response.StatusCode)
	}
	var problem problemDetails
	if err := json.NewDecoder(response.Body).Decode(&problem); err != nil {
		t.Fatalf("decode problem: %v", err)
	}
	if problem.Title != "temporarily_unavailable" || problem.Detail != "Movie search is temporarily unavailable" {
		t.Fatalf("problem = %+v", problem)
	}
}

func TestHandleTMDBSearch_CredentialUnavailableUsesGenericMemberError(t *testing.T) {
	t.Parallel()
	h, app, _, _ := setupEditMovieTest(t)
	h.tmdb = errorTMDBSearcher{err: integration.ErrCredentialUnavailable}

	response := doAs(t, app, httptest.NewRequest(http.MethodGet, "/api/v1/tmdb/search?query=Alien", nil), 1, "member")
	defer response.Body.Close()
	if response.StatusCode != fiber.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", response.StatusCode)
	}
	var problem problemDetails
	if err := json.NewDecoder(response.Body).Decode(&problem); err != nil {
		t.Fatalf("decode problem: %v", err)
	}
	if problem.Title != "temporarily_unavailable" || problem.Detail != "Movie search is temporarily unavailable" {
		t.Fatalf("problem = %+v", problem)
	}
}

func TestHandleTMDBSearch_QueueSaturationUsesGenericMemberError(t *testing.T) {
	t.Parallel()
	h, app, _, _ := setupEditMovieTest(t)
	h.tmdb = errorTMDBSearcher{err: &tmdbRequestQueueFullError{Limit: 32}}

	response := doAs(t, app, httptest.NewRequest(http.MethodGet, "/api/v1/tmdb/search?query=Alien", nil), 1, "member")
	defer response.Body.Close()
	if response.StatusCode != fiber.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", response.StatusCode)
	}
	var problem problemDetails
	if err := json.NewDecoder(response.Body).Decode(&problem); err != nil {
		t.Fatalf("decode problem: %v", err)
	}
	if problem.Title != "temporarily_unavailable" || problem.Detail != "Movie search is temporarily unavailable" {
		t.Fatalf("problem = %+v", problem)
	}
}
