# Runbook

The first sections cover the one-time switch to the per-member auth layer. They
apply when moviepickarr previously sat behind an Authelia forward-auth gate and
now owns its own login. If you run the app with no reverse proxy in front of it,
skip to [Hard cutover](#hard-cutover).

## Loosen the Authelia forward-auth gate

moviepickarr now authenticates every request itself: it mints its own sessions,
enforces its own login lockout, and reads the actor from the session cookie
instead of the URL. A forward-auth gate in front of the app can't tell one
member from another, and it blocks the very routes a logged-out member has to
reach to log in or claim their account. So the gate has to step aside for the
app.

This is a change to your proxy config, not to the app. The app code does not
know or care whether Authelia is there.

Recommended: drop the forward-auth gate for moviepickarr entirely. The app is a
standalone authenticator now, so a second gate in front of it only gets in the
way (and double-prompts your members).

If you want to keep Authelia in front during the transition, it must let these
paths through without the forward-auth check, or nobody can log in:

- `/api/v1/auth/*` — login, logout, session/password management, and the OIDC
  and claim endpoints. This is the whole auth surface.
- `/api/v1/auth/oidc/callback` — the OIDC provider redirects a logged-out
  browser here; it must be reachable without a session. (Under `/api/v1/auth/*`,
  so the wildcard rule already covers it.)
- `/api/v1/auth/claim/:token`, `/api/v1/auth/claim/:token/password`,
  `/api/v1/auth/claim/:token/oidc` — the claim flow a placeholder member walks
  before they have any credential. (These sit under `/api/v1/auth/*`, so a
  single `/api/v1/auth/*` bypass rule already covers them.)
- The SPA shell and its static assets, plus the `/login` and `/claim/*` pages —
  a logged-out visitor has to load the app to see the login form at all. In
  practice this means the whole front-end, which is why dropping the gate is
  cleaner than carving exceptions.

The app defends the auth surface on its own: per-account lockout (10 failed
attempts → 15-minute auto-expiring lock), uniform `401`s that don't reveal
whether a username exists, and origin-checked CSRF. It never relies on the proxy
for rate-limiting.

### OIDC redirect URI

If SSO is on, make sure the provider's registered redirect URI matches
`MPA_OIDC_REDIRECT_URL` exactly and points at
`<public-base-url>/api/v1/auth/oidc/callback`, and that the gate lets that
callback through per the rule above. See the SSO / OIDC section of
[`INSTALL.md`](INSTALL.md) for the local-http dev caveat.

## Hard cutover

There is no gradual rollout and no anonymous access after the switch. Auth is
mandatory and always on. The migration leaves every existing member as a
credential-less placeholder: their roster entry and authored movies survive, but
none of them can log in until they claim.

So the deploy is a hard cutover, in this order:

1. Set the break-glass admin env vars before the deploy: `MPA_ADMIN_NAME`,
   `MPA_ADMIN_USERNAME`, `MPA_ADMIN_PASSWORD` (all three or the seed is
   skipped). Point `MPA_ADMIN_NAME` at your existing roster name so the seed
   adopts that active member instead of creating a duplicate. An archived match
   fails boot and is never recredentialed. See the break-glass section of
   [`INSTALL.md`](INSTALL.md) for the matching rules and recovery path.
2. Deploy. Boot runs migrate → seed → serve: it applies migration `009` (with a
   pre-migration DB snapshot), seeds/adopts the admin, then starts serving. Boot
   fails loudly if the seed trio is set but seeding errors, and warns if no admin
   exists and no seed is configured.
3. Loosen the gate (above) so the login and callback routes are reachable.
4. The break-glass admin logs in with the seeded username/password.
5. Invite the rest. From the admin roster, use "Add & create link" for new
   members. For an existing placeholder, open the row menu and create an invite
   link. Each link is single-use and shown exactly once, with no resend, so
   deliver it out-of-band immediately. If a link is lost, use the same row menu
   to create a replacement. Open and expired state stays in the member's Login
   cell until the link is claimed, revoked, replaced, or dismissed. Onboarding
   links can set a password or link SSO. Password-reset links replace the
   password only.

The break-glass seed is idempotent and never overwrites an existing password, so
it is safe to leave the env vars set while the named member remains active.

## Integration credential key recovery

Admin-managed integration credentials are encrypted with the instance key at
`MPA_INTEGRATION_KEY_FILE`, or `<DB_FILE>.integration.key` when the variable is
unset. Back up that file with SQLite. A database backup without its matching key
does not contain usable Admin-managed TMDB credentials, Radarr API keys, or
Radarr webhook URLs.

If the key is missing or wrong, moviepickarr still starts. Affected integrations
report `Credential unavailable`; cached movie data and the core draw
workflow keep working. Radarr cannot add, search, grab, or reconcile an
Acquisition until its selected instance credential can be read. Webhook
deliveries with an unreadable URL fail without changing the Acquisition.

Recovery options:

1. Stop moviepickarr and restore the matching key file with owner-only `0600`
   permissions, then restart.
2. If the key cannot be restored, replace each affected secret in Admin. Use
   Admin > Integrations > TMDB for the TMDB API key. Use Admin > Integrations >
   Radarr > Setup for each Radarr API key. Use the Webhooks section to replace
   each destination URL, send a successful test, and enable it again. The old
   ciphertext is replaced only after the applicable save checks pass.

Do not delete the database row or cached metadata. Clearing a credential is an
Admin action and does not purge movie data.

## Radarr Acquisition recovery

Radarr Acquisition work is Admin-only and never blocks Reveal, Watch, or the
next draw. The attention badge counts every visible Acquisition that is not
Downloaded, Abandoned, or Canceled. It is persistent and cannot be dismissed.
Resolve it by completing the Acquisition or by abandoning it with a reason.

A member can instead cancel an Active wildcard. This marks its Acquisition
Canceled and closes only moviepickarr's local requirement. It does not delete,
stop, or change Radarr data or work. If remote work had started, inspect and
manage it in Radarr. The Canceled Acquisition stays terminal and does not reopen
from later Radarr observations.

The Acquisition worker runs once at process start and then every 30 seconds. It
checks at most 50 due locked targets per pass. Downloaded means that the exact
selected Radarr instance and movie report `hasFile`. A file in another instance
does not complete the Acquisition.

Use the Acquisition detail when action is required:

- `Connection failed`: restore the instance, or correct its URL or API key. For
  an unlocked target, review and confirm the target again. For a locked target,
  use Retry. A later successful request returns the instance to Connected.
- `Configuration invalid`: restore the snapshot's root folder, quality profile,
  or tags on the selected Radarr instance. An unlocked Acquisition can instead
  select a new valid preset.
- `Identity required`: for an unlocked target, search from the Acquisition and
  select the correct TMDB result. This is an Acquisition-only override and does
  not edit the movie. For a locked target, restore the exact movie in Radarr or
  abandon the Acquisition.
- `Add failed`: a definite unlocked failure returns the target to review. Correct
  the condition, then review and confirm the target again. If the add response
  was ambiguous, use `Check Radarr add`. This action only reads Radarr. It adopts
  the movie when it finds it. When Radarr proves that the movie is absent, it
  returns the target to review so an Admin can confirm a new add.
- A movie removed after Target confirmation keeps its locked target. Retry can
  recreate it only from that same snapshot. moviepickarr does not recreate it
  automatically and does not retarget it.
- `Release required`, `No releases`, or `Release failed`: run Interactive
  search when Radarr has no file or active queue item, then select a matched
  release. Refresh the search if its opaque result has expired.
- If a manual grab response was ambiguous, use `Check Radarr status`. This
  action reads the selected movie, queue, and history. It does not send the
  selected release again. Wait for Radarr evidence or select another release
  after moviepickarr reports a definite failure.
- `Import failed` or `Monitoring failed`: correct the Radarr condition and use
  Retry. Existing Radarr movies keep their original monitoring and other
  configuration.

Automatic search uses a durable local claim before moviepickarr sends the
Radarr command. After a timeout or restart, recovery checks the selected movie
and queue. If the handoff command was not recorded, it looks for a recent
`MoviesSearch` command that contains exactly that movie. It stores a match and
continues to observe it. If the handoff was already stored but its direct
command lookup is missing, recovery checks the matching command list and
continues observation without sending a new search. If the result stays
unknown, the claim remains active and Retry does not send another search.
Inspect Radarr, start work there if needed, or abandon the Acquisition after
you acknowledge that current activity is unavailable.

Preset selection includes a read-only exact-movie check. If it finds an existing
Radarr movie, moviepickarr adopts it without another confirmation and without
changing its root folder, quality profile, tags, minimum availability, or
monitoring. A file completes the Acquisition. If an active queue item exists,
moviepickarr observes it instead of starting competing work. Otherwise it
continues with the selected Manual or Automatic mode.

Abandon only when no file is expected. Every unresolved Acquisition can be
abandoned, including one with an in-progress local mutation. An unlocked idle
target has no Radarr activity to check. An unlocked mutation reports activity as
unavailable. A locked target gets a live movie and queue check. A reported file
completes the Acquisition instead of abandoning it.

The action requires a reason. Active or unavailable activity requires an exact
acknowledgement. Submission repeats the review and uses the Acquisition revision.
If the revision changes after this repeated review, the final update fails and
the Admin must review the current state. Abandonment does not remove or change
Radarr data and remains terminal if Radarr later imports a file.

Acquisition history keeps one compact summary per Current draw or Wildcard for
the movie's lifetime.
It is separate from the shared Runs page. Routine checks, release selections,
and individual retry actions do not create Integration runs.

## Radarr webhook delivery

Acquisition webhooks report only an actionable
`acquisition.action_required` transition. They do not report queue progress or
completed downloads. Each destination filters by reason, and the payload
includes the target label when known. Discord destinations use an embed and can
include one configured role mention. Generic destinations receive unsigned
JSON.

A destination must pass a saved-destination test before it can be enabled.
Changing its URL, payload format, or Discord role clears verification and
disables it. Set `MPA_PUBLIC_URL` to a valid public HTTP or HTTPS base URL to
include a link to the Admin Acquisition page. The sender does not follow
redirects.

Delivery is durable and independent from Acquisition state. The worker checks
up to 50 due deliveries every 15 seconds. It claims a delivery before sending
and recovers an expired claim on the next pass after a restart or interrupted
worker. A failed delivery retries after 1 minute, 5 minutes, 30 minutes, and then
2 hours, for at most five attempts. The last failure creates a Webhook health
warning. A later successful delivery or test clears the warning. Archiving the
destination also resolves it. There is no long-term delivery audit or manual
replay. A condition that is no longer current is retired before delivery.

The attempt is counted when the worker claims it, before the outbound request.
A process crash after the destination accepts the request can cause an
at-least-once duplicate. Generic receivers can deduplicate with the stable
`deliveryId` and `actionVersion` fields.

An unreadable encrypted webhook URL is a credential failure. It becomes
terminal on its first worker pass and is not replayed after the encryption key
is restored. A later successful test or real delivery clears the destination
warning only.

Delivery retention runs at startup and every 24 hours while the process is
running. Successful deliveries remain for 30 days. Terminal failures remain
until their warning is resolved, then remain for 90 more days. Webhook failures
never block, abandon, or otherwise change an Acquisition.
