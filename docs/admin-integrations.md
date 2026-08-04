# Admin-managed integrations

Status: planned (2026-08-04)

## Goal

Let an admin configure TMDB without editing deployment environment variables or
restarting moviepickarr. Keep deployment overrides authoritative and leave a
clear seam for integrations with richer setup, such as Radarr.

The first release covers TMDB configuration and integration run history. It does
not expose database paths, backups, logging, break-glass credentials, OIDC
credentials, or the OIDC redirect URL.

## Admin information architecture

Admin uses internal pages rather than dashboard cards:

- `/admin/members`: existing roster, and the target of `/admin`.
- `/admin/integrations`: a persistent list of integrations.
- `/admin/integrations/tmdb`: TMDB status, configuration, actions, and latest
  run.
- `/admin/runs`: run history across all integrations.

The integrations index stays in place even while TMDB is the only entry. Rows
show the integration name, current state, and latest activity. The TMDB page
uses the existing section and divider vocabulary. It does not introduce cards.
The run-history link opens `/admin/runs` with the TMDB filter applied.

Only admins may read or change integration configuration, test connections,
start or cancel runs, and read run history.

## Configuration model

Each setting has a typed value, built-in default, optional persisted Admin
value, optional environment value, validation, help text, and sensitivity. The
effective value is resolved in this order:

1. Environment value
2. Persisted Admin value
3. Built-in default

The API returns the effective non-secret value, active source, default, and
whether a dormant Admin fallback exists. Environment-controlled fields are
read-only. An admin may stage removal of a dormant fallback. Removing the
environment value later reactivates a retained fallback.

Secrets are write-only. The API reports whether a secret is configured, its
active source, and whether a dormant fallback exists. It never returns a value,
prefix, suffix, or hash.

The form uses one local draft and one `Save changes` action. Validation covers
the whole draft and persistence is atomic. Per-field `Use default` actions and
fallback removals join the same pending save. Leaving with unsaved changes asks
for confirmation. Autosave and a global reset are out of scope.

Every settings read includes a revision. A save against a stale revision is
rejected without merging or discarding the draft. A clean form may accept a
fresh server value; a dirty form retains its draft and reports that another
admin changed the settings.

Admin controls use typed values. Durations use a number and unit selector.
Special zero values use explicit controls such as `Scheduled refresh` and `All
cast members`. Environment syntax stays backward compatible.

Invalid values are rejected. Unusually aggressive values produce a confirmation
warning, including request pacing below 250 ms, refresh below 15 minutes, or
metadata freshness below 1 hour. Active environment values outside those
recommendations remain effective and show a warning.

## Shared framework and typed modules

The shared integration framework owns:

- Persisted configuration and revisions
- Environment precedence and defaults
- Encrypted secret storage and write-only API shapes
- Common status and connection-test behavior
- Atomic saves and live runtime replacement
- Common Admin navigation, source indicators, and run links
- The integration run ledger

Each typed integration module owns:

- Its configuration type and validation
- Its Admin form and help text
- Connection testing and error classification
- Runtime client construction and replacement
- Scheduling, pacing, retries, cancellation, and integration-specific actions

This is an internal code seam, not a runtime plugin system or schema-generated
form. A future Radarr module can test its base URL and API key, then fetch root
folders and quality profiles for typed selectors. It shares persistence, secret
handling, sources, saving, status, and history with TMDB.

See [ADR 0004](adr/0004-typed-integration-modules.md).

## TMDB settings

Human labels lead. The environment variable appears underneath as secondary
monospace text, along with the default and active source.

### Standard

| Label | Environment | Default | Help text |
| --- | --- | --- | --- |
| Enabled | `TMDB_ENABLED` | Enabled when a key exists | Allow moviepickarr to search TMDB and fetch artwork, metadata, and credits. |
| API key | `TMDB_API_KEY` | Not configured | Credential used to connect to TMDB. It is stored encrypted and never shown again. |
| Cast limit | `TMDB_ENRICH_CAST_LIMIT` | 15 | Maximum cast members stored for each movie. Choose all to keep the full cast. |
| Scheduled refresh | `TMDB_ENRICH_REFRESH_INTERVAL` | Every hour | How often moviepickarr checks for missing or stale TMDB data. |
| Metadata freshness | `TMDB_ENRICH_TTL` | 30 days | How old cached TMDB data may become before it is considered stale. |

TMDB cannot be active without a credential. Disabling it retains the configured
credential and all cached movie data. Clearing an Admin credential disables TMDB
when no environment credential is active. It does not purge metadata, credits,
or artwork paths.

### Advanced

| Label | Environment | Default | Help text |
| --- | --- | --- | --- |
| Request interval | `TMDB_ENRICH_MIN_INTERVAL_MS` | 250 ms | Minimum pause between TMDB requests. Higher values are gentler but make large refreshes slower. |
| Retry attempts | `TMDB_ENRICH_MAX_RETRIES` | 4 | Extra attempts after a temporary network or TMDB failure. |
| Retry backoff | `TMDB_ENRICH_BACKOFF_MS` | 500 ms | Initial wait before retrying a temporary failure. Later waits increase automatically. |
| Batch size | `TMDB_ENRICH_BATCH_LIMIT` | 200 | Maximum movies selected during one scheduled or manual stale refresh. |

Advanced settings start collapsed. Queue size and browser notification batching
remain environment-only implementation controls. Their short explanations stay
in deployment documentation, not the Admin UI:

| Environment | Default | Meaning |
| --- | --- | --- |
| `TMDB_ENRICH_QUEUE_SIZE` | 256 | Maximum newly added movies waiting for enrichment in memory. |
| `TMDB_ENRICH_BATCH_DEBOUNCE_MS` | 500 ms | Quiet period before a batch update is sent to connected browsers. |
| `TMDB_ENRICH_BATCH_MAX_WAIT_MS` | 2 seconds | Longest delay before browsers receive an update during continuous enrichment. |

## Secret storage and Docker secrets

Persisted integration secrets use AES-GCM with a dedicated random 32-byte
instance key stored outside SQLite with owner-only permissions. The key is
generated on first persisted-secret use. A deployment may instead provide a
secret-file path so the key can come from Docker secrets or another managed
secret mount. Direct environment credentials remain outside SQLite.

If the instance key is missing or wrong, moviepickarr starts with the affected
integration in `Credential unavailable`. Cached and core app behavior remains
available. Ciphertext is retained until an admin replaces the credential under
the current key.

See [ADR 0005](adr/0005-encrypt-persisted-integration-secrets.md).

## Save, test, and runtime behavior

`Test connection` uses the current draft without saving it. Its result stays on
the TMDB page and never creates a run record.

Saving a new key performs the same check:

- Confirmed authentication failure rejects the save and keeps the old key.
- A successful check saves with `Connected` status.
- A network or temporary TMDB failure may save with `Could not verify` status.

All settings apply without restart. In-flight requests finish with their old
configuration snapshot. New work receives the replacement runtime.

When TMDB becomes enabled with its first valid key, moviepickarr starts one
normal missing-or-stale refresh. A refresh-interval change reschedules from save
time. TTL, cast limit, pacing, retry, and batch changes do not start work.
Lowering TTL takes effect on the next stale refresh. Increasing the cast limit
requires normal expiry or `Re-enrich all`.

A confirmed authentication failure during normal use stops the active run,
suspends new TMDB work, and reports `API key rejected`. It does not delete the
credential or cached data. Replacing the key or passing a later connection test
resumes work. Network errors and rate limits use normal retry and later-refresh
behavior instead of suspending the integration.

## TMDB status and actions

The TMDB page shows:

- State: `Disabled`, `Connected`, `Could not verify`, `Error`, or `Credential
  unavailable`
- Last checked and next scheduled check
- Last successful refresh
- Current or latest run with progress and counts
- `Test connection`
- `Refresh stale now`
- `Re-enrich all`, with confirmation
- `Cancel run` while a library-wide run is active

Only one library-wide integration run may execute at a time. Scheduled and
manual runs do not overlap. Per-movie enrichment for newly added movies may
continue through the paced worker. Cancellation stops scheduling new movies,
lets the active request finish, keeps completed updates, and records the run as
cancelled.

Saving settings never silently starts a full re-enrichment.

When TMDB is disabled or suspended, cached movie data still renders. Member
search reports `Movie search is temporarily unavailable` without exposing the
configuration or credential failure. Admins see the specific cause and recovery
action on the TMDB page.

## Integration run ledger

The shared ledger records actual integration work, including single-movie
enrichment. Scheduled and manual scans that select no subjects update `Last
checked` but do not create a run.

A run records:

- Integration, operation, trigger, and initiating admin when applicable
- Configuration revision
- Status and start/end timestamps
- Total, processed, succeeded, failed, skipped, and remaining counts
- Sanitized error summary and a bounded failed-subject sample

Statuses are `Running`, `Completed`, `Completed with errors`, `Failed`,
`Cancelled`, and `Interrupted`. A process restart changes unfinished runs to
`Interrupted`. Connection tests are excluded. There is no first-release action
to retry failed subjects; missing or stale movies return in the next refresh.

Keep 12 months of history with a safety cap of 10,000 runs per integration. The
shared history table is newest-first and filters by integration, operation,
status, and trigger. The ledger does not schedule work, replay jobs, store raw
remote traffic, or accept arbitrary non-integration jobs.

See [ADR 0006](adr/0006-integration-run-ledger.md).

## Build slices

1. Add typed effective-configuration resolution, revisioned persistence, and
   encrypted secret storage.
2. Move TMDB client and worker construction behind a replaceable typed runtime.
3. Add Admin routes, integration list, TMDB form, validation, source display,
   and connection testing.
4. Add live status, manual actions, cooperative cancellation, and member-facing
   unavailable behavior.
5. Add the run ledger, retention, history filters, and current-run progress.
6. Update installation, environment, backend-layout, product, and design docs;
   verify migrations, backend behavior, Admin permissions, desktop, mobile,
   keyboard use, and both themes.

## Explicit non-goals

- Editing database, backup, or logging configuration in the app
- Exposing break-glass or OIDC configuration
- A generic settings table passed into integration code
- Runtime-loaded plugins
- A distributed job queue or general background-job framework
- Automatic replay of failed integration runs
- Returning, masking, or previewing stored secrets
