# Admin-managed integrations

Status: implemented (2026-08-07)

## Goal

Let an Admin configure TMDB and Radarr without restarting moviepickarr. Keep
TMDB deployment overrides authoritative. Give Radarr a dedicated Admin workflow
for routing a drawn movie to one verified target and arranging its initial file.

The Admin surface does not expose database paths, backups, logging, break-glass
credentials, OIDC credentials, or the OIDC redirect URL.

## Admin information architecture

Admin uses internal pages rather than dashboard cards:

- `/admin/roster`: existing roster, and the target of `/admin`.
- `/admin/integrations`: integration catalog for TMDB and Radarr.
- `/admin/integrations/tmdb`: TMDB status, configuration, actions, and latest
  run.
- `/admin/integrations/radarr`: Radarr Acquisitions and history.
- `/admin/integrations/radarr/acquisitions/:id`: one Acquisition workflow.
- `/admin/integrations/radarr/setup`: Radarr instances and Acquisition presets.
- `/admin/integrations/radarr/webhooks`: Generic and Discord destinations.
- `/admin/runs`: shared Integration run history. Individual Acquisitions are
  excluded.

Admin uses one nested index for Roster, Integrations, and Runs. TMDB and Radarr
appear inside the active Integrations branch, so integrations do not add a
second rail or repeat an integration heading. Radarr adds a local section switch
for Acquisitions, Setup, and Webhooks. At widths of 901px and up, the index stays
on the left and every selected Admin page scrolls independently on the right.
Below 901px, the index becomes horizontal and the document owns vertical
scrolling. Integration and Acquisition routes remain shareable deep links.

One vertical indicator slides between Roster, Integrations, and Runs with the
same duration and easing as the primary tabs. It keeps the standard 22px line on
Roster and Runs, then grows across the full Integrations branch while an
integration is visible. The child slot derives from the same row step, including
coarse-pointer targets, so the line and branch share one geometry. The selected
integration uses a weighted, gold-tinted label without a selected fill or
marker. The shell has no repeated Admin heading. TMDB can link to `/admin/runs`
with its filter applied. Radarr Acquisitions remain on the Radarr page.

Each desktop destination uses the full row as its pointer target and shares the
same background hover treatment. Below 901px, the current leaf has a gold bottom
marker, including TMDB and Radarr inside the Integrations branch.

Route changes keep one current-page marker and announce the newly selected
section. Touch-sized desktop rows update the indicator's step from the same CSS
row variable, so the line stays centered without measuring link geometry.

Only Admins may read or change integration configuration, test connections,
start or cancel runs, read run history, or see and control Acquisitions. A
persistent attention count appears on the top-level Admin destination and the
Radarr entry. No Acquisition identity or state is sent through member-facing
SSE events.

## TMDB configuration model

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
- Common Admin navigation and status conventions
- TMDB source indicators and run links
- The integration run ledger for work that has a recorded execution outcome

Each typed integration module owns:

- Its configuration type and validation
- Its Admin form and help text
- Connection testing and error classification
- Runtime client construction and replacement
- Scheduling, pacing, retries, cancellation, and integration-specific actions

This is an internal code seam, not a runtime plugin system or schema-generated
form. Radarr uses the shared encryption and Admin conventions, but owns typed
instances, presets, Acquisitions, and webhooks. Individual Acquisition actions
and routine status checks are domain workflow, not Integration runs.

See [ADR 0004](adr/0004-typed-integration-modules.md).

## TMDB settings

Human labels lead in a compact settings ledger. The active source stays visible.
Help, the environment variable, and the built-in default live behind the row's
info control so values configured once do not carry permanent explanatory copy.
The help surface is portalled above the Admin scroller and clamped to the
viewport. Opening it never changes the setting row's height.

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

The TMDB page keeps the current connection state and last successful refresh in
its compact summary. Routine activity lives under an `Activity details`
disclosure:

- State: `Disabled`, `Connected`, `Could not verify`, `Error`, or `Credential
  unavailable`
- Last library scan, last connection test, and next scheduled scan
- Last successful refresh
- Current or latest run with progress and counts
- `Test connection`
- `Refresh stale now`
- `Re-enrich all`, with confirmation
- `Cancel run` while a library-wide run is active

Active runs, configuration warnings, errors, and recovery reasons remain
visible without opening the disclosure.

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

## Radarr setup

Radarr setup uses two Admin-managed resource types. There are no Radarr
environment defaults or automatic routing rules.

A Radarr instance is one separately configured installation. Each active
instance has a unique name, HTTP or HTTPS base URL, write-only API key,
connection state, and revision. The create and edit flow must reach Radarr and
load its catalog before it saves. A later outage marks the instance Offline but
does not delete its configuration. The same movie can exist in several
instances, such as 1080p, 4K, and anime. moviepickarr does not infer which one
to use.

An edit can reuse the stored API key only when the URL scheme and host stay the
same. A scheme or host change requires the Admin to enter the API key again
before moviepickarr contacts the new endpoint.

An Acquisition preset contains:

- One Radarr instance
- One root folder
- One quality profile
- Zero or more tags
- One minimum-availability value
- Manual or Automatic Acquisition mode

The root folders, quality profiles, and tags come from the live selected
instance. Saving verifies that the instance is reachable, the root folder is
accessible, and every selected value still exists. A preset that later drifts
cannot be selected until the configuration is valid again. A stale revision is
rejected so one Admin cannot silently overwrite another Admin's change.

Archiving removes an instance or preset from future selection without erasing
its historical identity. Archiving an instance also archives its presets. An
instance cannot be archived while an unresolved Acquisition targets it.
Acquisitions store a snapshot of the selected preset, so later edits do not
change earlier target history.

Radarr API keys use the same AES-GCM instance key as other persisted integration
secrets. The API reports only whether a key is configured. It never returns or
masks the value.

## Radarr Acquisition workflow

One durable Acquisition is created atomically with each draw. Pooled candidates
do not get Acquisitions. Its identity and state remain concealed from every
Admin view and webhook until Reveal. A restart can resume this pending record.
If startup cannot restore the concealed record, boot fails before serving.
The draw, Reveal, Watch, and next-draw workflows never wait for Radarr.

After Reveal, an Admin selects one preset. moviepickarr snapshots its instance,
root folder, quality profile, tags, minimum availability, and mode. It resolves
identity by stored TMDB ID first. If there is no TMDB ID, it resolves the stored
IMDb ID through Radarr and verifies the returned match. It does not use title
and year as an automatic fallback. When exact identity cannot resolve, an Admin
can search Radarr and select a TMDB result for this Acquisition only.

Preset selection performs an exact, read-only Radarr check. If the movie already
exists in that instance, moviepickarr adopts it immediately, preserves its
effective configuration, and locks the target without another Admin prompt. If
the movie does not exist, Target review shows the Acquisition identity and the
complete selected target. The Admin can change the preset until confirmation.
Confirmation adds the new movie and locks the target. A locked Acquisition
cannot move to another instance. If Radarr later removes the movie, an explicit
retry can recreate it only from the same snapshot.

An add response can be ambiguous after Radarr accepts the request. The
Acquisition stays unlocked with its durable add claim. The contextual `Check
Radarr add` action uses the normal retry endpoint and only reads Radarr. A found
movie is adopted and locks the target. Proven absence clears the claim and
returns the target to review. Only a later confirmation can send a new add.

Existing Radarr movies keep their root folder, quality profile, tags, minimum
availability, and monitoring. They are adopted during preset selection without
confirmation. `hasFile` completes the Acquisition immediately. An active queue
item is observed instead of replaced. With no file or queue, Manual mode offers
Interactive search and Automatic mode asks Radarr to run one search. The
selected target must report `hasFile`; a copy in another Radarr instance does
not count.

For a new movie, Manual mode adds it unmonitored with search disabled. An Admin
starts Interactive search in moviepickarr and chooses one matched release. An
approved release can be sent directly. Radarr-rejected matched releases remain
available in a collapsed section and require an explicit override. Unmatched or
unparsed results cannot be sent. Result IDs are opaque and expire after 30
minutes; moviepickarr does not return or persist a URL, magnet, hash, or GUID.
After a successful grab, moviepickarr enables monitoring. Automatic mode adds a
new movie monitored, then starts a Radarr movie search. It first stores a
durable `searching` claim. After a timeout or restart, recovery checks the
selected movie and queue. It also looks for an active or recent `MoviesSearch`
command that contains exactly the selected movie. A found command is stored and
observed when the original handoff was not recorded. If a stored command can no
longer be read directly, moviepickarr checks the matching command list and
continues to observe it without changing the durable claim. An unknown result
keeps the claim and does not permit another automatic search. An Admin must
inspect Radarr, start work there, or abandon after acknowledging unavailable
activity.

A manual grab also starts with a durable `grabbing` claim. If the response is
ambiguous, the contextual `Check Radarr status` action uses the normal retry
endpoint to read the selected movie, queue, and history. It never sends the
cached release again. A file, queue item, or matching history proves that Radarr
accepted the request. Otherwise moviepickarr keeps observing until it can report
a definite release failure.

The reconciler observes only the locked target. It maps file, queue, command,
and history state to Needs release, Waiting for Radarr, Queued, Downloading,
Importing, Downloaded, or Action needed. Elapsed time alone does not create an
error. If Radarr has already started a replacement, moviepickarr observes it and
does not send a duplicate search or actionable message.

If the locked Radarr movie no longer matches the Acquisition identity,
moviepickarr reports `identity_required`. Identity search cannot change a locked
target. The Admin must restore the exact movie in Radarr or abandon the
Acquisition.

## Radarr attention and history

Every revealed Acquisition that is not Downloaded or Abandoned contributes to
the persistent Admin attention count, including healthy automatic progress and
Needs preset. The reminder is not dismissible. Marking the Current draw Watched
does not clear it and does not block any movie workflow.

The Acquisitions section shows active work and searchable Downloaded or
Abandoned history. Each detail keeps one compact summary per draw: identity and
target snapshots, the effective configuration of an adopted movie, the latest
selected release summary, attempt count, latest failure, milestones, and any
abandonment reason. It does not store a full event log, every
release attempt, raw remote responses, or release URLs.

Abandonment requires a reason. Review returns `not_applicable` for an unlocked
idle target and `unavailable` for an unlocked in-progress mutation. For a locked
target, moviepickarr reads the exact movie and its queue. A file changes the
Acquisition to Downloaded. Active work returns `active`; a failed live check
returns `unavailable`; and a successful check with no work returns `inactive`.
A failed Radarr check does not block abandonment.

Submission repeats the review and uses a revision comparison. A current
`active` or `unavailable` value must exactly match `acknowledgedActivity`. A new
actionable risk refreshes the warning for another confirmation. A state change
to `inactive` needs no risk acknowledgement. A revision change after the
repeated review still rejects the final update. Every unresolved mutation state
can be abandoned. The check and abandonment do not remove or change Radarr
data. Abandonment clears the attention count and does not reopen when a file
appears later.

See [ADR 0007](adr/0007-admin-routed-single-target-radarr-acquisition.md).

## Radarr Acquisition webhooks

Admins can configure multiple named Generic or Discord destinations. A
destination starts disabled. It must pass a saved-destination test before it can
be enabled. Changing its URL, payload format, or Discord role makes it unverified
and disabled again. Webhook URLs are encrypted and write-only.

There is one event contract: `acquisition.action_required`. Each destination
filters only the action-needed reasons it wants. A transition sends the movie
title, reason, target label when known, and an Admin link when `MPA_PUBLIC_URL`
is configured. At least one reason is required. A new destination receives only
later transitions, not existing conditions. A condition that is resolved before
delivery is retired. Queue progress and completed downloads are not events.
The reason values are `preset_required`, `identity_required`,
`release_required`, `configuration_invalid`, `connection_failed`, `add_failed`,
`no_releases`, `release_failed`, `import_failed`, and `monitoring_failed`.

Generic destinations receive unsigned JSON. Discord destinations receive an
embed with the movie, reason, target, Admin link, and optional role mention.
Payloads exclude root paths, Radarr URLs, API details, and release URLs.

A Generic delivery has this stable shape:

```json
{
  "event": "acquisition.action_required",
  "data": {
	"deliveryId": 105,
    "acquisitionId": 42,
	"actionVersion": 3,
    "movieTitle": "Example Movie",
    "reason": "release_required",
    "target": "1080p Manual",
    "adminUrl": "https://movies.example.com/admin/integrations/radarr/acquisitions/42"
  }
}
```

For a Discord incoming-webhook URL, choose the Discord format instead of
Generic. moviepickarr builds the required `embeds` payload and restricts
mentions to the configured role ID.

Delivery is durable and does not affect Acquisition state. Failures retry with
bounded backoff for up to five claimed attempts. A process crash after the
destination accepts a request can cause an at-least-once duplicate. Generic
receivers can deduplicate with the stable `deliveryId` and `actionVersion`.
Exhausted retries create a separate Webhook health warning. A successful real
delivery or test clears it. Delivered diagnostics remain for 30 days. Resolved
terminal failures remain for 90 days. There is no long-term delivery audit.
An unreadable encrypted webhook URL is terminal on the first worker pass. Key
recovery does not replay that delivery. A later successful test or real delivery
only clears the destination warning.

## Integration run ledger

The shared ledger records integration operations with an execution outcome,
including single-movie TMDB enrichment. Scheduled and manual scans that select
no subjects update `Last checked` but do not create a run. Radarr Acquisition
actions and routine reconciliation do not create runs. They remain in the
Acquisitions section, so the shared Runs page and its retention stay unchanged.

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
shared history page is a newest-first register of finished results, filtered by
integration, operation, and result. Active progress stays on the owning
integration page. Each lean result row opens a details modal with timing,
trigger, complete counts, and any failure summary or subjects. Internal run IDs
and configuration revisions remain in the ledger and API, but are not shown in
the Admin interface. The integration catalog supplies operation IDs and labels
for the shared Type filter; unknown valid identifiers still receive readable
fallback labels. The ledger does not schedule work, replay jobs, store raw
remote traffic, or accept arbitrary non-integration jobs.

Retention runs at startup and once per day while the process remains up.

See [ADR 0006](adr/0006-integration-run-ledger.md).

## Admin API

All endpoints require an authenticated admin session. Errors use the existing
`application/problem+json` response shape. Settings responses never include a
credential value.

| Method | Path | Purpose |
| --- | --- | --- |
| `GET` | `/api/v1/integrations` | List integrations with state, latest activity, and run-operation labels. |
| `GET` | `/api/v1/integrations/tmdb` | Read effective TMDB settings, sources, timestamps, warnings, and the latest run. |
| `PUT` | `/api/v1/integrations/tmdb` | Validate and atomically save a revisioned settings draft. |
| `POST` | `/api/v1/integrations/tmdb/test` | Test the unsaved draft without creating a run. |
| `POST` | `/api/v1/integrations/tmdb/runs` | Start a stale refresh or confirmed full re-enrichment. |
| `GET` | `/api/v1/integrations/radarr/attention` | Read the unresolved revealed Acquisition count. |
| `GET` | `/api/v1/integrations/radarr/acquisitions` | Read active and historical Acquisitions. |
| `GET` | `/api/v1/integrations/radarr/acquisitions/:id` | Read one visible Acquisition. |
| `PUT` | `/api/v1/integrations/radarr/acquisitions/:id/preset` | Select and preview one target snapshot. |
| `POST` | `/api/v1/integrations/radarr/acquisitions/:id/confirm` | Confirm and lock the reviewed target. |
| `POST` | `/api/v1/integrations/radarr/acquisitions/:id/identity-search` | Search for an Acquisition-only identity override. |
| `PUT` | `/api/v1/integrations/radarr/acquisitions/:id/identity` | Select an Acquisition-only TMDB identity. |
| `POST` | `/api/v1/integrations/radarr/acquisitions/:id/releases/search` | Run Interactive search. |
| `POST` | `/api/v1/integrations/radarr/acquisitions/:id/releases/:resultId/grab` | Ask Radarr to grab one cached result. |
| `POST` | `/api/v1/integrations/radarr/acquisitions/:id/retry` | Run the contextual `Check Radarr add`, `Check Radarr status`, or locked retry action. |
| `POST` | `/api/v1/integrations/radarr/acquisitions/:id/abandon/review` | Return `{ acquisition, activity }` after a live read. Activity is `active`, `inactive`, `unavailable`, `not_applicable`, or `complete`. |
| `POST` | `/api/v1/integrations/radarr/acquisitions/:id/abandon` | End an Acquisition with `{ reason, acknowledgedActivity }`. A current `active` or `unavailable` risk must match the acknowledgement. |
| `GET`, `POST` | `/api/v1/integrations/radarr/instances` | List or create verified instances. |
| `PUT`, `DELETE` | `/api/v1/integrations/radarr/instances/:id` | Update or archive an instance. |
| `GET` | `/api/v1/integrations/radarr/instances/:id/options` | Load live root folders, quality profiles, and tags. |
| `GET`, `POST` | `/api/v1/integrations/radarr/presets` | List or create validated presets. |
| `PUT`, `DELETE` | `/api/v1/integrations/radarr/presets/:id` | Update or archive a preset. |
| `GET`, `POST` | `/api/v1/integrations/radarr/webhooks` | List or create webhook destinations. |
| `PUT`, `DELETE` | `/api/v1/integrations/radarr/webhooks/:id` | Update or archive a destination. |
| `POST` | `/api/v1/integrations/radarr/webhooks/:id/test` | Test and verify a saved destination. |
| `POST` | `/api/v1/integrations/radarr/webhooks/test` | Test an unsaved draft without verifying it. |
| `GET` | `/api/v1/integration-runs` | Read newest-first, filtered finished results with keyset pagination. |
| `DELETE` | `/api/v1/integration-runs/:runID` | Request cooperative cancellation of the active library run. |

Run-history pages contain at most 50 finished results by default and 100 when
explicitly requested. The cursor is the last row's start time and ID. This keeps
query and render cost bounded as the ledger approaches its retention cap.

Shared TMDB pacing admits at most 32 pending interactive requests. Excess
searches fail with the generic temporary-unavailable response. The bounded
library worker has reserved admission, so a member search burst cannot turn a
refresh into a string of queue-full failures. Run progress is persisted every
10 subjects or 2 seconds, and successful movie updates use the existing
debounced browser invalidation batch.

## Implementation map

- The shared integration layer owns secret encryption and TMDB configuration
  and run contracts.
- TMDB owns its revisioned runtime, enrichment workers, scheduler, and run
  history.
- Radarr owns its revisioned instances and presets, durable per-draw
  Acquisitions, reconciler, Interactive search cache, and webhook outbox.
- Admin routes enforce role checks for every integration configuration and
  Acquisition operation.

## Explicit non-goals

- Editing database, backup, or logging configuration in the app
- Exposing break-glass or OIDC configuration
- A generic settings table passed into integration code
- Runtime-loaded plugins
- A distributed job queue or general background-job framework
- Automatic replay of failed integration runs
- Returning, masking, or previewing stored secrets
- Sending one Acquisition to several Radarr instances
- Maintaining later release upgrades after the first imported file
- Download-complete or routine-progress Acquisition webhooks
- Plex library availability checks
