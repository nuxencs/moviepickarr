# Development

`make dev` runs the Vite dev server and the Go backend side by side in a tmux
session:

```bash
make dev          # live dev: Vite + Go, split-screen
make test         # run both suites: go test -race + vitest
make lint         # lint Go + frontend
make precommit    # format + fix + lint before committing
```

`make test` runs the Go suite (`go test -race`) and the frontend vitest suite
(the pure reducers: `drawMachine`, `sseConnection`, `sseInvalidations`). Both
also run in CI on every push and pull request (`.github/workflows/ci.yml`). To
run just the web tests, use `bun run test` from `web/`.

Vite hot-reloads the frontend only. After Go changes, restart the backend pane
before verifying anything.

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
