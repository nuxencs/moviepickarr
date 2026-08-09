# moviepickarr

A private web app for a friend group to choose movies together: everyone stashes
movies, promotes a few into a shared pool, and the app draws one at random.

## Language

### Product wording

**Movie**:
The canonical noun for every title in product copy, code, tests, and documentation.

**Draw workflow**:
The draw, reveal, and watch sequence. Use the specific action name when possible.

**Brand wordmark**:
The lowercase `moviepickarr` name shown in the top-left navigation. Site body copy
describes the action or system directly instead of naming the app as the actor.

### People

**Member**:
A person in the friend group. Has a personal stash and up to 3 movies in the
pool.
_Avoid_: user (survives only in code)

**Roster**:
The current set of members. Former members can still appear in history as
adders of watched movies.
_Avoid_: Roster as the name of the admin surface. The roster is a collection
shown on the Admin page, which is why the tab reads Admin while the section
heading on it reads Roster.

**Adder**:
The member who added a movie. A movie keeps its adder forever, including into
the watched history and stats ("Added by").
_Avoid_: picker, picked by, owner

**Next up**:
The member whose turn it is to run the draw workflow: draw, reveal, and mark the
current draw watched. Enforced, not just shown in the hero: only the next-up
member (or an admin) can draw, reveal, or mark watched. The turn holds on one
member across the whole draw → reveal → watch cycle and rotates to the next
member on watch, but only when the pool still has movies left and more than one
member exists. The server serializes these three commands from authorization
through lifecycle event publication. A watch therefore owns the outgoing turn
until rotation and `movie:watched` are published. The watched movie and next-up
handoff commit in one transaction; a failed handoff leaves the current draw and
turn unchanged for retry.
_Avoid_: next picker

### Identity

**Admin**:
A member holding the admin role: can create, delete/archive, and restore
members, lock the pool, and manage integrations and settings. Every other member holds the plain member
role. Role is app-owned and single-valued, never derived from a credential.

**Local login**:
A member's username and password credential. Optional: a member may have a local
login, a linked identity, both, or neither. The username is a stable login
handle, separate from the display name.

**Linked identity**:
A member's connection to the external SSO provider, held alongside any local
login. Optional and additive.
_Avoid_: OIDC account, SSO user

**Link**:
Binding a member to an SSO identity.

**Invite**:
A single-use, expiring link an admin sends so a member can set up their login.
An explicit password-reset invite can instead recover an existing local login.
It ends in one of four states: open, used (claimed), revoked, or expired. Each
issuance is one immutable generation. Exactly one unused, unrevoked generation
can be current for a member, even after it expires, and admin actions address
that generation through a random public handle. Open and expired generations
remain visible in the roster's Login cell, including password-reset invites for
credentialed members, because an admin can still replace, revoke, or dismiss
them. Only the link's hash is stored, so an issued link can never be shown a
second time by anyone.
_Avoid_: registration, signup

**Open**:
An invite that has been issued, not yet claimed, not revoked, and not past its
expiry. The one state whose link still works.
_Avoid_: active, pending, valid

**Expired**:
An invite whose window lapsed before anyone claimed it. Its link is dead, but
the admin may still need to create a replacement, which is why it stays on
screen.
_Avoid_: stale, lapsed (as a noun)

**Dismiss**:
An admin clearing an expired invite from the roster's Login cell. Implemented
as a revoke, so the invite is genuinely gone rather than hidden: there is no
dismissed-but-alive state.
_Avoid_: hide, archive, ignore

**Claim**:
A member redeeming an invite to set a password and/or link SSO. An onboarding
claim turns a placeholder into a login-capable member. A password-reset claim
replaces an existing password, ends the member's existing sessions, and cannot
be exchanged for a new SSO link.

**Placeholder**:
A roster member with no login credential yet: visible on the roster and usable
as an adder, but unable to log in until they claim.
_Avoid_: pending user, inactive user

**Archived**:
A removed member who authored movies: their `users` row survives with
`archived_at` set so watch-history attribution holds, but their credentials,
sessions, and invites are stripped and every authentication lookup treats them
as absent. Restore strips any residual authentication rows again, clears
`archived_at`, and inserts a fresh claim invite in the same transaction. A
member who authored nothing is hard-deleted instead, not archived.
_Avoid_: deleted user, disabled user

**Session**:
A server-side, revocable login for a member, carried by an opaque cookie. A
member holds one per device they signed in on, sees their own (never anyone
else's), and can end any single one or all of them at once. A session is live
while it is inside both its absolute cap and its idle window; past either it is
no longer a device the member is signed in on. Member-facing actions address a
session through an immutable random handle; the database row id never crosses
the API boundary.
_Avoid_: token, cookie (those are how a session travels, not what it is)

**Device**:
How a member reads one of their own sessions: the browser and platform it was
created from. Descriptive only, derived from what the browser said about
itself at sign-in, so it identifies a session to its owner and nothing more.
Network addresses are neither captured nor shown.

### Movie lifecycle

**Stash**:
A member's backlog of movies. Unlimited; not eligible for the draw. Private is
write-scoped only: the member is the sole curator (add, edit, promote/demote),
but the contents stay readable by any authenticated member.

**Pool**:
The shared set of movies eligible for the next draw. Each member may hold at
most 3 movies in it.

**Pool lock**:
An admin switch that fixes the pool's composition ahead of a draw. While it is
set, no movie enters or leaves the pool: promote, demote, and deleting a pooled
movie are all refused. The stash is untouched by it (adds, edits, and deletes
there carry on), and an unrevealed draw outranks it, so a frozen tile reports
the draw rather than the lock.

**Promote / Demote**:
Moving a movie from stash to pool, and back. The only transitions a member
performs by hand.

**Draw**:
The app's random selection of one pooled movie. Members add
and promote; only the app draws. Exactly one movie wins; the rest stay pooled.
_Avoid_: pick, roll, spin (the spin is the reel animation, not the selection)

**Current draw**:
The drawn movie waiting to be watched. At most one exists at a time.
_Avoid_: current pick, current movie

**Reel**:
The slot-machine animation every client plays while a draw is in flight,
spinning across the pool's candidates before landing on the winner.

**Reveal**:
The moment the draw's identity is settled on screen: the client that triggered
the draw confirms, or the countdown elapses. Until then the reel keeps
spinning on every client.

**Held draw**:
The drawn movie still shown as pooled because the draw isn't revealed yet. The
row is already `current`, but every pool read hands it back until the reveal, so
a missing tile can't give the winner away mid-reel. For as long as it's held the
pool is frozen (no demote, no delete, on any tile) and it still counts against
its adder's pool cap.

**Settle**:
The reel resting on the winner, awaiting confirmation. The scroll is over but
the draw is not yet revealed: the drawer sees the OK countdown, everyone else
waits for the reveal (the confirm, or the server's auto-reveal deadline).

**Watched**:
A movie the group has finished. Watched history is permanent; it never loses
its adder and is never cascaded away.

**Watched library**:
The browsable, searchable grid/list of all watched movies on the Movies tab.

### Integrations

**Integration**:
An external service connected to moviepickarr to extend what the app can do.
TMDB is an integration; each integration owns its admin-managed settings.

**Integration run**:
One execution of an integration operation, started by a schedule, an admin, app
startup, or an application event such as adding a movie. It has a recorded
outcome and may cover one or many subjects. Individual Acquisition actions and
routine status checks are not Integration runs; an explicit Radarr batch or
maintenance operation can be one.

### TMDB

**Enrichment**:
The background fetch of TMDB data for a movie. Async: a movie is fully usable
before enrichment lands, it just renders with a placeholder poster.

**Metadata**:
The enriched display fields of a movie (poster, backdrop, overview, runtime,
genres, rating, tagline). Display only; never part of a movie's identity.

**Credits**:
The people on a movie: cast in billing order (capped) and crew filtered to a
job whitelist (Director, Writer, Screenplay, Original Music Composer, Director
of Photography).

### Radarr

**Radarr instance**:
One configured Radarr installation that represents a media variant or
collection boundary. The same movie can exist in more than one instance.
_Avoid_: Radarr client

**Acquisition**:
The process of arranging a media file for a Current draw. It covers the initial
grab, not ongoing maintenance of that movie's releases.
_Avoid_: preparation, delivery

**Pending acquisition**:
A durable Acquisition created with a Current draw. It remains concealed until
Reveal and waits for an Admin to select an Acquisition preset.
_Avoid_: pending download, unassigned download

**Acquisition preset**:
A reusable Admin-managed combination of one Radarr instance, root folder,
quality profile, tags, minimum availability, and Acquisition mode.
_Avoid_: route preset, Radarr preset

**Target review**:
The final Admin confirmation of an Acquisition target before moviepickarr adds
a movie that does not already exist in the selected Radarr instance. An exact
existing movie is adopted during preset selection without this confirmation.
_Avoid_: preset preview

**Acquisition target**:
The snapshot of the one selected Acquisition preset used for an Acquisition.
It is separate from later edits to the reusable preset.
_Avoid_: destination, route

**Acquisition identity**:
The movie identity snapshot used for Radarr work. It is independent of later
edits to the movie.
_Avoid_: lookup identity

**Downloaded**:
The terminal Acquisition state in which the selected target reports an
imported media file.
_Avoid_: available, playable

**Manual acquisition**:
An Acquisition mode in which an Admin chooses a release through Interactive
search.
_Avoid_: manual download

**Automatic acquisition**:
An Acquisition mode in which moviepickarr asks Radarr to search for a release.
_Avoid_: automatic download

**Existing Radarr movie**:
A movie already present in the selected Radarr instance when its Acquisition
target is reviewed.
_Avoid_: imported movie

**Interactive search**:
A Radarr search shown in moviepickarr so an Admin can choose the release that
Radarr grabs.
_Avoid_: manual search

**Acquisition status**:
The Admin-facing lifecycle label for an Acquisition. A separate Action-needed
reason identifies a condition that needs Admin work.
_Avoid_: download status

**Action-needed reason**:
The typed cause of an Acquisition condition that an Admin can act on in
moviepickarr.
_Avoid_: webhook event type, error code

**Acquisition history**:
The Admin-only durable summary of an Acquisition. It is a compact workflow
record, not an Integration run or full event log.
_Avoid_: integration run, event log

**Acquisition reminder**:
A persistent Admin attention item for an Acquisition that is neither Downloaded
nor Abandoned.
_Avoid_: notification, toast

**Abandoned acquisition**:
An Acquisition that an Admin explicitly ends without a file. It is a terminal
state and includes the Admin's reason.
_Avoid_: dismissed acquisition, cancelled download

**Acquisition webhook**:
An outbound Generic or Discord notification for an Acquisition condition that
requires action in moviepickarr.
_Avoid_: Radarr webhook

**Webhook destination**:
A named Generic or Discord endpoint that can receive Acquisition webhooks.
_Avoid_: webhook client

**Discord destination**:
An Acquisition webhook destination that renders notifications as Discord
embeds.
_Avoid_: Discord notification

**Webhook delivery**:
One durable Acquisition condition and destination pairing. Its record tracks up
to five send attempts.
_Avoid_: notification run

**Webhook-health warning**:
A persistent Admin warning for a Webhook destination that has a terminal
delivery failure.
_Avoid_: Acquisition reminder

### Stats

**Window**:
The time range stats aggregate over — a preset (24h … all-time) or a custom
date range.

**Leaderboard**:
The per-member watch counts for the active window and filters. Every roster
member always has a row, zeroed or not.
