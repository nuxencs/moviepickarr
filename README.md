# 🎬 moviepickarr

**Whose turn is it to pick the movie?** Never argue about it again.

moviepickarr is a small, private web app for a group of friends — "the gang" — to
run a shared movie-night rotation. Everyone keeps their own list of films they
want to watch, promotes their top picks into a shared pool, and the app draws
the next movie at random. It then remembers what you watched and turns it into
fun stats.

It's self-hosted and made for a handful of people who know each other — not a
public site. You run it once, share the link with your friends, and that's it.

---

## How it works

The whole app is built around one simple loop:

1. **Stash** — every member keeps a personal list of movies they'd like to see.
   Add them by searching (titles, posters and details come from
   [TMDB](https://www.themoviedb.org/) automatically).
2. **Pool** — each member promotes up to **3** movies from their stash into the
   shared pool. This is everyone's "I'm in the mood for these" shortlist.
3. **Pick** — when it's movie night, the app draws one movie from the pool at
   random. No more endless scrolling or debating.
4. **Watch & track** — mark it watched, and it moves into the gang's watched
   library along with who picked it and when.
5. **Stats** — see who's watched the most, your busiest movie nights, favourite
   genres, most-watched directors and actors, and more.

## The three tabs

- **🍿 Movies** — the heart of the app. A big cinematic banner shows the current
  pick with its poster, runtime, rating and a one-tap "pick next" button. Below
  it sits the shared pool and the full watched library (searchable by title or by
  who picked it). Tap any movie for a detail view with its overview, "Directed
  by / Written by" credits and a scrollable cast strip.
- **👥 Users** — the members of the gang. Each person has their pool slots (max 3)
  and their own searchable stash. Add new movies here.
- **📊 Stats** — a deep dive into the gang's watch history over any time window.
  A member leaderboard, weekday/hourly activity, hours watched, average rating,
  top genres, most-watched directors and actors, and a release-decades timeline.
  Filter the whole page by genre, year, who picked it, or specific actors and
  crew — click any face to drill in.

Movies are automatically enriched with rich metadata from TMDB: posters,
backdrops, runtimes, ratings, genres, taglines, overviews and full cast & crew.
Anything not yet fetched falls back to a clean placeholder, so the app always
looks complete.

---

## Run it yourself

moviepickarr ships as a single program with everything bundled in — the website,
the server and the database all live in one place. Pick whichever setup you like.

### What you'll need

- A **TMDB API key** (free) so movies get posters, cast and stats. Grab one at
  [themoviedb.org → Settings → API](https://www.themoviedb.org/settings/api).
  *It's optional* — the app runs without it, but you'll miss the artwork and the
  metadata-powered stats.
- Either **Docker** (easiest), or **Go 1.26+** and **[Bun](https://bun.sh)** to
  build from source.

### Option A — Docker (recommended)

The repo includes a ready-to-go `docker-compose.yml`. Open it, drop in your TMDB
key, then:

```bash
docker compose up -d
```

Your data lives in `~/.config/moviepickarr` on your machine, so it survives
restarts and updates.

### Option B — Build from source

```bash
# 1. Build the web UI and the server into a single binary
make build

# 2. Add your TMDB key (optional but recommended)
echo "TMDB_API_KEY=your_key_here" > .env

# 3. Run it
./bin/moviepickarr
```

### Then open it

Visit **http://localhost:3030** and you're in. Add a couple of members on the
**Users** tab, stash some movies, promote your picks — and let the app choose
movie night for you. 🎉

---

## For developers

Want to hack on it? `make dev` runs the frontend (with hot reload) and the
backend side-by-side in a tmux session:

```bash
make dev          # live dev: Vite + Go, split-screen
make test         # run the Go test suite
make lint         # lint Go + frontend
make precommit    # format + fix + lint before committing
```

**Under the hood:**

- **Backend** — [Go](https://go.dev) with the
  [Fiber](https://gofiber.io) web framework and an embedded
  [SQLite](https://sqlite.org) database (no external DB to set up). Movie
  enrichment runs in a background worker and live updates are pushed to the
  browser over Server-Sent Events.
- **Frontend** — [React 19](https://react.dev) +
  [TypeScript](https://www.typescriptlang.org), with
  [TanStack Router](https://tanstack.com/router) &
  [Query](https://tanstack.com/query), [Tailwind CSS](https://tailwindcss.com)
  and a bespoke, cinematic design system (see
  [`docs/DESIGN.md`](docs/DESIGN.md)).

The design and product decisions live in [`docs/`](docs/) —
[`PRODUCT.md`](docs/PRODUCT.md) for what it is and
[`DESIGN.md`](docs/DESIGN.md) for how it looks and why.

---

## Configuration

Everything is optional except the TMDB key. Set these as environment variables
(or in a `.env` file next to the binary):

| Variable | What it does | Default |
| --- | --- | --- |
| `TMDB_API_KEY` | Enables posters, cast & metadata-powered stats | *(unset — enrichment off)* |
| `LOG_LEVEL` | Minimum log level (`trace`…`fatal`) | `info` |
| `LOG_FORMAT` | `json` (production) or `console` (colourised dev) | `json` |
| `TMDB_ENRICH_CAST_LIMIT` | How many cast members to store per movie (`0` = all) | `15` |
| `TMDB_ENRICH_REFRESH_INTERVAL` | How often to refresh metadata (`0` disables) | `1h` |
| `TMDB_ENRICH_TTL` | How stale metadata can get before a refresh | `720h` |

See [`.env.example`](.env.example) for the full list. Logging is documented in
[`docs/LOGGING.md`](docs/LOGGING.md), and there are a few more enrichment-worker
knobs in [`docs/backend-layout.md`](docs/backend-layout.md).

---

<sub>This product uses the TMDB API but is not endorsed or certified by TMDB.</sub>
