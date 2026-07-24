# Development

`make dev` runs the Vite dev server and the Go backend side by side in a tmux
session:

```bash
make dev          # live dev: Vite + Go, split-screen
make test         # run both suites: go test -race + vitest
make lint         # lint Go + frontend
make precommit    # format + fix + lint before committing
```

`make test` runs the Go suite (`go test -race`) and the frontend vitest suites. Both
also run in CI on every push and pull request (`.github/workflows/ci.yml`). To
run just the web tests, use `bun run test` from `web/`.

The web tests are split into two vitest projects (`web/vitest.config.ts`):

- `node`: `src/**/*.test.ts` in a plain node environment, no DOM. This is where
  most tests belong: the pure reducers and helpers (`drawMachine`, `sseConnection`,
  `sseInvalidations`, `sseInvalidationQueue`, `search`, `useGridMetrics`) take their
  environment as data.
- `dom`: `src/**/*.render.test.tsx` in jsdom with Testing Library, for behaviour
  that only exists once a component renders and has no pure seam below it (the
  draw reel's remount resume is the first one). Drive the DOM the way a member
  would, by role and text; don't assert on class names or internal state.

Run one project with `bunx vitest run --project dom` (or `--project node`).

Vite hot-reloads the frontend only. After Go changes, restart the backend pane
before verifying anything.

## Dev fixtures

`make dev/fixtures` populates a full developer world in one command so you don't
have to hand-build a roster and a movie backlog:

```bash
make dev/fixtures         # load fixtures (refuses if the DB already has data)
make dev/fixtures-reset   # wipe all data, then load fixtures from empty
```

It writes to the same DB the server uses (`DB_FILE`, or `moviepickarr.db` by
default), so the usual flow is `make dev/fixtures` then `make dev`. The load runs
in one transaction and is deterministic: the same roster, movie counts, and
per-window watched history every run.

What you get:

- 5 members with working logins (1 admin + 4 plain), plus a placeholder (no
  login yet) and an archived member (authors watched history so attribution is
  exercised).
- ~200 movies across every state: ~120 watched (with `watched_at` spread across
  the stats windows, from the last 24h out to a few years), ~75 in stashes, and
  a handful pooled. No current draw, so the app opens ready-to-draw.
- An active turn holder and an unlocked pool.

Log in with any seeded member. Usernames are the lowercased name (`ada`, `ben`,
`cleo`, `dev`, `erin`; `ada` is the admin) and every one shares the password
`devpassword`.

Two notes:

- Leave `MPA_ADMIN_*` unset in dev. The fixtures seed their own admin login, so
  the break-glass admin seed isn't needed and a matching `MPA_ADMIN_USERNAME`
  would collide with a seeded login on boot.
- Movies carry real TMDB ids but no cached metadata. With a `TMDB_API_KEY` set,
  the enrichment worker fills posters and details on the next `make dev`;
  without one they render with placeholder posters.

The movie dataset (`internal/devfixtures/data/movies.json`) is a committed list
of real TMDB titles. To regenerate it (rarely needed, since TMDB ids are stable),
pull the top-rated list with your key:

```bash
set -a; . ./.env; set +a
for p in $(seq 1 25); do \
  curl -s -H "Authorization: Bearer $TMDB_API_KEY" \
    "https://api.themoviedb.org/3/movie/top_rated?language=en-US&page=$p"; \
done | jq -s 'add as $x | [.[] | .results[] | select(.adult|not) |
  {tmdb_id: .id, title, year: (.release_date[0:4]|tonumber? // 0)}] | unique_by(.tmdb_id)' \
  > internal/devfixtures/data/movies.json
```

## Stack

- Backend: [Go](https://go.dev) with the [Fiber](https://gofiber.io) web
  framework and an embedded [SQLite](https://sqlite.org) database. Movie
  enrichment runs in a background worker, and live updates reach the browser
  over Server-Sent Events.
- Frontend: [React 19](https://react.dev) with
  [TypeScript](https://www.typescriptlang.org),
  [TanStack Router](https://tanstack.com/router) and
  [Query](https://tanstack.com/query), [Tailwind CSS](https://tailwindcss.com),
  and a bespoke design system.

## Docs

- [`../CONTEXT.md`](../CONTEXT.md): the project glossary. Use its terms in
  code, copy and issues.
- [`PRODUCT.md`](PRODUCT.md): what the app is and who it is for.
- [`DESIGN.md`](DESIGN.md): the design system. Read it before any web/UI work.
- [`backend-layout.md`](backend-layout.md): how the backend is laid out.
- [`LOGGING.md`](LOGGING.md): logging reference.
