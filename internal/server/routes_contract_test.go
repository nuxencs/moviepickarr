package server

import (
	"testing"

	"github.com/gofiber/fiber/v2"
)

// Locks the routes the Members board calls (web/src/api/APIClient.ts, `users` block).
// The /users* rename in PR #96/#110 once 404'd every Members-tab request.
func TestFrontendMemberMovieRoutesRegistered(t *testing.T) {
	app := fiber.New()
	// Registration only takes method values off the handler; it never calls them,
	// so a zero handler is enough to enumerate the route table.
	registerV1Routes(app.Group("/api/v1"), &handler{})

	registered := map[string]bool{}
	for _, r := range app.GetRoutes() {
		registered[r.Method+" "+r.Path] = true
	}

	// Each entry maps a frontend APIClient call to the route it must hit.
	required := []string{
		"GET /api/v1/members",                 // users.getAll (board)
		"DELETE /api/v1/members/:memberID",    // members.remove (delete member)
		"GET /api/v1/members/:memberID/pool",  // users.getPool
		"GET /api/v1/members/:memberID/stash", // users.getStash
		"POST /api/v1/movies",                 // users.addMovie
		"PUT /api/v1/movies/:movieID",         // users.updateMovie
		"DELETE /api/v1/movies/:movieID",      // users.deleteMovie
		"POST /api/v1/movies/:movieID/move",   // users.moveMovie
	}
	for _, want := range required {
		if !registered[want] {
			t.Errorf("frontend depends on route %q but it is not registered", want)
		}
	}

	// The old /users* paths must stay gone: their return signals a half-done rename.
	for _, gone := range []string{
		"GET /api/v1/users",
		"POST /api/v1/users/:userID/movies",
		"POST /api/v1/users/:userID/movies/:movieID/move",
	} {
		if registered[gone] {
			t.Errorf("stale route %q is registered; the frontend no longer calls it", gone)
		}
	}
}
