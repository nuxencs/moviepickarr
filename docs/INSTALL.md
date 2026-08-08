# Install

moviepickarr ships as a single binary. The web UI, the server and the SQLite
database are all embedded, so there is nothing else to set up.

## Requirements

- An optional TMDB API key, free at
  [themoviedb.org](https://www.themoviedb.org/settings/api) under Settings,
  API. The key is optional: without it the app still works, but movies show
  placeholder posters and the metadata stats stay empty.
- An optional Radarr installation and API key. Radarr is not required for the
  movie-night workflow. When configured, it can arrange a file for each drawn
  movie before the next movie night.
- Either Docker, or Go 1.26+ and [Bun](https://bun.sh) to build from source.

## Docker

The repo contains a ready [`docker-compose.yml`](../docker-compose.yml). Start
it, then configure the optional services from Admin > Integrations:

```bash
docker compose up -d
```

Data is stored in `~/.config/moviepickarr` on the host, so it survives
container restarts and updates.

## From source

```bash
# 1. Build the web UI and the server into a single binary
make build

# 2. Run it
./bin/moviepickarr
```

## First steps

Open http://localhost:3030. Add members on the Members tab, stash some movies,
promote up to 3 per member into the pool, and draw.

## Configuration

TMDB and Radarr can be configured from Admin > Integrations without a restart.
TMDB typed settings also remain available as environment variables for
deployment-owned overrides. An environment value wins over its Admin value and
makes that field read-only in the app. Radarr instances, presets, and webhook
destinations are Admin-managed. Set environment variables directly, or place
them in a `.env` file next to the binary:

| Variable | What it does | Default |
| --- | --- | --- |
| `TMDB_API_KEY` | Enables posters, cast and metadata stats | unset (enrichment off) |
| `TMDB_ENABLED` | Enables or disables TMDB while retaining its credential and cache | enabled when a key exists |
| `MPA_INTEGRATION_KEY_FILE` | AES-GCM instance key file for Admin-managed credentials and webhook URLs | `<DB_FILE>.integration.key` |
| `MPA_PUBLIC_URL` | Public moviepickarr base URL used in Acquisition webhook links | unset (links omitted) |
| `MPA_ADMIN_NAME` | Break-glass admin: display name to create or adopt | unset (seed skipped) |
| `MPA_ADMIN_USERNAME` | Break-glass admin: login username | unset (seed skipped) |
| `MPA_ADMIN_PASSWORD` | Break-glass admin: login password | unset (seed skipped) |
| `MPA_OIDC_ISSUER` | SSO provider issuer URL (discovery base) | unset (SSO off) |
| `MPA_OIDC_CLIENT_ID` | OIDC client id registered with the provider | unset (SSO off) |
| `MPA_OIDC_CLIENT_SECRET` | OIDC client secret (confidential client) | unset (SSO off) |
| `MPA_OIDC_REDIRECT_URL` | Callback URL, `<base>/api/v1/auth/oidc/callback` | unset (SSO off) |
| `MPA_OIDC_TX_SECRET` | Key for the encrypted OIDC transaction cookie | unset (random per boot) |
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

### Admin-managed TMDB

Open Admin > Integrations > TMDB to set the API key, test the current draft,
and save all settings atomically. The API key is write-only. Once saved, the UI
shows only whether it is configured and which source is active.

Admin-managed credentials are stored in SQLite as AES-GCM ciphertext. The
separate instance key defaults to `<DB_FILE>.integration.key`; the Docker volume
in the sample Compose file keeps it beside the database. Back up both files.
Set `MPA_INTEGRATION_KEY_FILE` to a mounted secret path when the deployment owns
the key. See [`RUNBOOK.md`](RUNBOOK.md#integration-credential-key-recovery) for
recovery behavior.

The Admin TMDB page also exposes connection status, scheduled and manual
refreshes, cancellation, and run history. `TMDB_ENRICH_QUEUE_SIZE`,
`TMDB_ENRICH_BATCH_DEBOUNCE_MS`, and `TMDB_ENRICH_BATCH_MAX_WAIT_MS` remain
environment-only worker controls.

### Admin-managed Radarr

Open Admin > Integrations > Radarr > Setup. Add each Radarr installation as a
separate instance with a name, base URL, and API key. A new or edited instance
must be reachable and must accept its API key before it can be saved. This
supports setups such as separate 1080p, 4K, and anime instances.
Enter the API key again when an edit changes the URL scheme or host. This keeps
the stored write-only key from being sent to a different endpoint.

Create one or more Acquisition presets after an instance is saved. Each preset
selects exactly one instance, root folder, quality profile, optional tags,
minimum availability, and Acquisition mode. moviepickarr fetches root folders,
quality profiles, and tags from the selected instance. Minimum availability and
Acquisition mode are local typed selections. A preset save checks the live
instance and verifies every selected remote value. The same checks run when the
preset is used. Archive a preset or instance to remove it from future selection
while keeping its name in Acquisition history. Archiving an instance also
archives its presets.

The modes control only the initial grab:

- Manual adds a new movie unmonitored and does not start a search. An Admin can
  run an Interactive search in moviepickarr, select a matched release, and ask
  Radarr to grab it. moviepickarr then enables monitoring.
- Automatic adds a new movie monitored and asks Radarr to search immediately.

Only the drawn winner gets an Acquisition. The record is created with the draw
and remains concealed until Reveal. After Reveal, an Admin selects one preset.
If the exact movie already exists in that instance, moviepickarr adopts it and
locks the target without another prompt. It preserves the movie's effective
settings. If the movie has a file, the Acquisition completes immediately.
Otherwise moviepickarr observes its queue or starts the selected mode. If the
movie does not exist, the Admin reviews and confirms the exact target before
moviepickarr adds it.

The Admin and Radarr navigation show a persistent attention count until the
selected instance reports a file or an Admin abandons the Acquisition with a
reason. This does not block Reveal, Watch, or the next draw. Completed and
abandoned entries remain in the Admin-only Acquisition history. They do not
appear on the shared Runs page.

Admin > Integrations > Radarr > Webhooks can send actionable Acquisition states
to multiple Generic or Discord destinations. Save a destination disabled, send
a successful test, then enable it. Destinations can filter by action-needed
reason. Discord uses an embed and can mention one role. Set `MPA_PUBLIC_URL` to
the externally reachable moviepickarr base URL if the message must link to the
Acquisition. For example, use `https://movies.example.com` without an Admin
path. When this value is unset or invalid, the webhook still sends but omits
the Admin link.

Plex availability is not part of this integration. Download completion uses
Radarr `hasFile` on the selected instance. See the deferred
[Plex availability note](research/plex-availability.md).

### Break-glass admin

Onboarding is invite-only, so a fresh deploy needs one bootstrapped admin. Set
all three of `MPA_ADMIN_NAME`, `MPA_ADMIN_USERNAME`, and `MPA_ADMIN_PASSWORD`
(all or nothing) and, on boot, moviepickarr creates or adopts an admin member
with a working local login. It matches `MPA_ADMIN_NAME` against existing member
names case-insensitively: no match creates a fresh admin, exactly one match
adopts that member and makes them an admin, and an ambiguous multi-match is
skipped and logged. Archived members are never adopted. An exact archived match
fails boot instead of restoring access implicitly. Choose an unused seed name
and username to bootstrap a fresh admin, then restore the archived member
through the admin roster.

The seed is idempotent and never overwrites an existing password, so it is safe
to leave the vars set while the named member remains active. Boot fails if the
trio is set but seeding errors. A failed create or adoption leaves the member,
role, and local login unchanged, so the operator can correct the conflict and
restart. Boot warns if no active admin exists and no seed is configured.

### SSO / OIDC

moviepickarr can act as an OIDC relying party against a single external
provider, so members log in (or link their account) with their existing SSO
instead of a local password. It runs the standard relying-party path (discovery,
PKCE, ID-token verification) and reads identity only from the verified ID token:
no refresh tokens, no `offline_access`, no userinfo call.

Enablement is presence-derived. Set all four of `MPA_OIDC_ISSUER`,
`MPA_OIDC_CLIENT_ID`, `MPA_OIDC_CLIENT_SECRET`, and `MPA_OIDC_REDIRECT_URL` and
SSO turns on; leave any unset and it stays off (the `/oidc/*` routes return 404
and the login and claim pages hide the SSO controls). Scopes are fixed to
`openid profile email`. Register `MPA_OIDC_REDIRECT_URL` with the provider
exactly as set; it must be `<public-base-url>/api/v1/auth/oidc/callback`.

`MPA_OIDC_TX_SECRET` is optional. It keys the short-lived encrypted cookie that
carries the login state across the provider round trip. Left unset, a random key
is generated at boot, so a restart invalidates any login still in flight (fine
in practice). Set a stable value (any length; it is folded to a 256-bit key) to
survive restarts and to share the key across multiple instances behind a load
balancer.

Logout is session-only: signing out ends the moviepickarr session but does not
call the provider's `end_session_endpoint`, so the IdP's own SSO session is left
alone.

Local dev over http: point `MPA_OIDC_REDIRECT_URL` at `http://localhost:3030/...`
and register that redirect URI with your provider. If you terminate TLS at a dev
proxy, forward `X-Forwarded-Proto: https` so the session and transaction cookies
get their `Secure` flag. On plain http they are set without it: the documented
residual of running raw http is a session cookie with no `Secure` flag.

### Deploying behind a reverse proxy

The app authenticates every request itself, so a forward-auth gate (for example
Authelia) in front of it can't tell one member from another and blocks the login
and callback routes. If you run one, see [`RUNBOOK.md`](RUNBOOK.md) for loosening
the gate and the one-time cutover steps.
