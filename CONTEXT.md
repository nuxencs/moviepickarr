# moviepickarr

A private web app for a friend group to run a shared movie-night
rotation: everyone stashes movies, promotes a few into a shared pool, and the
app draws one at random for movie night.

## Language

### People

**Member**:
A person in the friend group. Has a personal stash and up to 3 movies in the
pool.
_Avoid_: user (survives only in code/API names and the Users tab)

**Roster**:
The current set of members. Former members can still appear in history as
adders of watched movies.

**Adder**:
The member who added a movie. A movie keeps its adder forever, including into
the watched history and stats ("Added by").
_Avoid_: picker, picked by, owner

**Next up**:
The member whose turn it is to run movie night: mark the current draw watched
and trigger the next draw. Rotates through the roster after each draw, but only
when the pool still has movies left and more than one member exists. A
convention shown in the hero, not enforced by the app.
_Avoid_: next picker

### Movie lifecycle

**Stash**:
A member's private backlog of movies. Unlimited; not eligible for the draw.

**Pool**:
The shared set of movies eligible for the next draw. Each member may hold at
most 3 movies in it.

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
