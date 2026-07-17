# Install

moviepickarr ships as a single binary. The web UI, the server and the SQLite
database are all embedded, so there is nothing else to set up.

## Requirements

- A TMDB API key, free at
  [themoviedb.org](https://www.themoviedb.org/settings/api) under Settings,
  API. The key is optional: without it the app still works, but movies show
  placeholder posters and the metadata stats stay empty.
- Either Docker, or Go 1.26+ and [Bun](https://bun.sh) to build from source.

## Docker

The repo contains a ready [`docker-compose.yml`](../docker-compose.yml). Enter
your TMDB key there, then:

```bash
docker compose up -d
```

Data is stored in `~/.config/moviepickarr` on the host, so it survives
container restarts and updates.

## From source

```bash
# 1. Build the web UI and the server into a single binary
make build

# 2. Add your TMDB key (optional)
echo "TMDB_API_KEY=your_key_here" > .env

# 3. Run it
./bin/moviepickarr
```

## First steps

Open http://localhost:3030. Add members on the Users tab, stash some movies,
promote up to 3 per member into the pool, and draw.

## Configuration

Everything except the TMDB key has a sensible default. Set these as
environment variables, or in a `.env` file next to the binary:

| Variable | What it does | Default |
| --- | --- | --- |
| `TMDB_API_KEY` | Enables posters, cast and metadata stats | unset (enrichment off) |
| `DB_FILE` | Path to the SQLite database file | `moviepickarr.db` (working dir) |
| `DB_BACKUP_MAX` | Pre-migration snapshots to keep next to the DB (`0` disables) | `3` |
| `LOG_LEVEL` | Minimum log level (`trace` to `fatal`) | `info` |
| `LOG_FORMAT` | `json` (production) or `console` (colourised dev) | `json` |
| `TMDB_ENRICH_CAST_LIMIT` | How many cast members to store per movie (`0` = all) | `15` |
| `TMDB_ENRICH_REFRESH_INTERVAL` | How often to refresh metadata (`0` disables) | `1h` |
| `TMDB_ENRICH_TTL` | How stale metadata may get before a refresh | `720h` |

See [`.env.example`](../.env.example) for the full list. Logging is documented
in [`LOGGING.md`](LOGGING.md), and the remaining enrichment-worker settings in
[`backend-layout.md`](backend-layout.md).
