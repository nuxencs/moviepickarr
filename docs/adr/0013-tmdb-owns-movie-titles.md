# ADR 0013: TMDB owns movie titles, admins own corrections

Status: accepted (2026-10-24)

## Context

A movie's title came from the TMDB search result once, when a member added it.
After that, the adder could change the title and the movie link in the Edit
movie dialog. This caused three problems:

- Custom titles drifted from TMDB. The same movie could read differently for
  different adders, and nothing brought it back.
- The link field was not cosmetic. It set the movie's TMDB or IMDb identity, so
  an edit could point a watched movie at a different film and clear its
  metadata and credits.
- `POST /movies` still accepted a pasted IMDb or TMDB link with a free-text
  title, although the UI only adds from TMDB search.

The one real use of editing was repair: fixing a wrong pick, or a wrong watched
date. Delete and re-add fixes a stash or pool movie, but delete refuses watched
and current movies, so those had no other repair path.

## Decision

TMDB is the only source of a movie's title. Every enrichment (on add, on the
scheduled refresh, and on Re-enrich all) writes the TMDB en-US `title` to
`movies.title` in the guarded `ApplyEnrichment` update. A TMDB rename applies
everywhere, including watched history and stats. There is no lock for each
movie. Enrichment also stores the TMDB `original_title` in `movie_metadata`.
It stays in the database and is not in any API response until a feature needs
it.

Members cannot edit a movie. `PUT /movies/:id`, the Edit movie dialog, and
`parseMovieLink` are removed. `POST /movies` accepts only `tmdbId`. The client
title stays as a temporary title until enrichment replaces it.

Repair becomes an admin-only Movie correction on a new Maintenance tab
(`/admin/maintenance`), through `PUT /movies/:id/correction`. An admin picks
a new identity from TMDB search (any status) or changes the watched date
(watched movies only). The adder never changes. The correction is refused while
a draw is unrevealed, or when another movie already has the TMDB id.

Every Current draw and Wildcard has an Acquisition that copies the movie identity
at draw time (ADR 0007), even with no Radarr instance configured. An identity
correction rewrites that copy in the same transaction while the target is not
locked, because nothing exists in Radarr yet. After target lock, or while a
Radarr call is in flight, the identity correction is refused: Radarr holds the
old movie, so the admin abandons the Acquisition first. A watched date
correction never touches Acquisitions.

## Considered options

- **Keep custom titles, add a "reset to TMDB" action.** Rejected: two sources
  for one field, and the reset would need to run for each movie.
- **Keep identity correction for the adder, with a TMDB picker.** Rejected:
  corrections are rare, and an identity change on a watched movie rewrites
  group history, so it belongs with the admin.
- **Refuse every identity correction while an Acquisition is open.** Rejected:
  every Current draw has an open Acquisition, so this blocked the common repair
  case. Copying the identity only at target lock instead of at draw time would
  remove the rewrite (#383).

## Consequences

- The first Re-enrich all after this change overwrites every custom title. The
  backup that the app takes before each migration keeps the old values.
- An admin who wants to correct the identity of a movie whose Acquisition is
  already target-locked must abandon the Acquisition first.
