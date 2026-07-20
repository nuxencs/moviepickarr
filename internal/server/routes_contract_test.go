package server

import (
	"testing"

	"github.com/gofiber/fiber/v2"
)

// The frontend (web/src/api/APIClient.ts, the `users` block) depends on this
// exact set of member and movie routes for the Members board. They were renamed
// from /users* to /members* + /movies* in the authz reshape (PR #96/#110), but
// the frontend kept calling the old /users* paths, so every Members-tab request
// 404'd and the board hung in its loading skeleton. This test locks the contract
// so a later rename can't silently break the board again without failing here.
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

	// The old /users* movie paths must stay gone: their return would signal a
	// half-done rename that reintroduces the original bug.
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
