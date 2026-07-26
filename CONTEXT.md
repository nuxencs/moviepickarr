# moviepickarr

A private web app for a friend group to run a shared movie-night
rotation: everyone stashes movies, promotes a few into a shared pool, and the
app draws one at random for movie night.

## Language

### People

**Member**:
A person in the friend group. Has a personal stash and up to 3 movies in the
pool.
_Avoid_: user (survives only in code)

**Roster**:
The current set of members. Former members can still appear in history as
adders of watched movies.

**Adder**:
The member who added a movie. A movie keeps its adder forever, including into
the watched history and stats ("Added by").
_Avoid_: picker, picked by, owner

**Next up**:
The member whose turn it is to run movie night: draw, reveal, and mark the
current draw watched. Enforced, not just shown in the hero: only the next-up
member (or an admin) can draw, reveal, or mark watched. The turn holds on one
member across the whole draw → reveal → watch cycle and rotates to the next
member on watch, but only when the pool still has movies left and more than one
member exists.
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
_Avoid_: registration, signup

**Claim**:
A member redeeming an invite to set a password and/or link SSO, turning a
placeholder into a login-capable member.

**Placeholder**:
A roster member with no login credential yet: visible on the roster and usable
as an adder, but unable to log in until they claim.
_Avoid_: pending user, inactive user

**Archived**:
A removed member who authored movies: their `users` row survives with
`archived_at` set so watch-history attribution holds, but their credentials,
sessions, and invites are stripped and they drop off every active read (roster,
stats, rotation). Restore clears `archived_at` and re-issues a claim link. A
member who authored nothing is hard-deleted instead, not archived.
_Avoid_: deleted user, disabled user

**Session**:
A server-side, revocable login for a member, carried by an opaque cookie.

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
The app's random selection of one pooled movie for movie night. Members add
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

### Stats

**Window**:
The time range stats aggregate over — a preset (24h … all-time) or a custom
date range.

**Leaderboard**:
The per-member watch counts for the active window and filters. Every roster
member always has a row, zeroed or not.
