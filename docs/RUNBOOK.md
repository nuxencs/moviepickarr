# Runbook: auth cutover

Operational notes for the one-time switch to the per-member auth layer. This
covers a deploy where moviepickarr previously sat behind an Authelia
forward-auth gate and now owns its own login. If you run the app with no reverse
proxy in front of it, skip straight to [Hard cutover](#hard-cutover).

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
does not contain a usable Admin-managed TMDB credential.

If the key is missing or wrong, moviepickarr still starts. TMDB reports
`Credential unavailable`; cached movie data and core movie-night behavior keep
working. Recovery options:

1. Stop moviepickarr and restore the matching key file with owner-only `0600`
   permissions, then restart.
2. If the key cannot be restored, open Admin > Integrations > TMDB and replace
   the API key. The retained ciphertext is replaced only after the new key
   passes the save flow.

Do not delete the database row or cached metadata. Clearing a credential is an
Admin action and does not purge movie data.
