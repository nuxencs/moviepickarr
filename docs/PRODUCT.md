# moviepickarr: Product

Register: product. An app/tool, so design serves the task and earned
familiarity beats novelty.

## What it is
A small private web app for a friend group to choose movies together. Each
member keeps a personal stash of movies and promotes up to 3 into a shared
pool. The app draws a random movie from the pool, then tracks the watched
history and stats. Movies are
enriched with TMDB metadata (poster, backdrop, runtime, rating, genres, tagline,
overview, external links, and cast/crew credits). An optional Radarr integration
lets an Admin arrange the initial media file for a Current draw or Wildcard.

## Audience
A handful of friends who know each other. Private, authenticated by obscurity,
not a public or marketing surface. Desktop-first, with a dedicated mobile/touch
pass on top (bottom tab bar, touch-reachable actions; see DESIGN.md §13).

## Primary surfaces (tabs)
- Movies: the cinematic hero (Current draw, Active wildcard, actions, Next up), the pool
  grid, and the watched library (grid/list, searchable by title or adder). The
  Movies page stays browse-first; metadata drill-downs live on Stats. A movie's
  detail modal shows "Directed by / Written by" credit lines and a horizontally
  scrollable cast strip (TMDB headshots, billing order); each cast card links to
  the person's TMDB page.
- Members: each with their pool slots (max 3) and a searchable stash; add via
  TMDB search.
- Stats: watch stats over a time window (member leaderboard where every member
  always has a row, zero or not, whatever the window or filters; weekday and
  hourly activity; custom date range), plus a TMDB deep-dive over the watched
  subset: hours-watched and average-rating KPIs (with an average-runtime
  sub-line), a horizontally-scrolling rail of the actual movies behind the count
  (click a poster for its detail modal), a top-genres donut, most-watched
  director and actor rails, and a release-decades timeline. One filter system
  sits under the header: the time presets, the watch-year quick-select (snaps
  the custom window to a calendar year), genre, release year or whole decade
  (mutually exclusive), "Added by" (the movie's adder), and multi-select
  Actors / Crew people filters. People filters are any-of within a list and
  AND across filters; actors match cast credits, crew matches any whitelisted
  crew job (so filtering by a director also counts their writer credits).
  Clicking a person card on a rail toggles them in the matching people filter
  and drills the whole page down (every aggregate is computed server-side over
  the filtered subset); each card also carries a corner link to the person's
  TMDB page.
- Admin: a shared shell for the Roster, Integrations, and Runs destinations.
  The primary app Members tab keeps its existing name. A nested index selects
  TMDB or Radarr without adding a second integration rail. Every desktop Admin
  page scrolls inside that shell. The TMDB detail owns typed settings, source
  indicators, connection testing, manual refreshes, and current-run progress.
  Radarr owns Admin-only Acquisitions, multi-instance setup and presets, and
  Generic or Discord actionable webhooks. Its persistent attention badge remains
  until the selected target reports a file, an Admin abandons the Acquisition,
  or the group cancels its Active wildcard.
  Runs lists finished integration operations only, with a lean summary and
  per-result details modal. Individual Radarr Acquisition work stays on the
  Radarr page and does not change Runs. Routine activity is available on demand.
  TMDB environment overrides stay visible and read-only.

## Wildcard watches

After Reveal, any Turn participant can select one Active wildcard from the
existing Pool or Stashes, or directly from TMDB. An existing movie keeps its
Adder. A direct TMDB selection uses the selecting member as its Adder. Selection
creates a visible Pending acquisition immediately.

Guests can browse the same app and manage their own Stash. They cannot promote
movies to the Pool or hold Next up. They cannot use Draw or Reveal. They also
cannot mark a Wildcard as Watched, select a Wildcard, or cancel a Wildcard.

The group must watch or cancel the Active wildcard before it can mark the
Current draw Watched. Watching it adds the movie to the Watched library but does
not close its Acquisition, complete the Current draw, or rotate Next up. The
group can then select another Wildcard for the same Current draw, with no limit
on sequential Wildcard watches.

Cancellation restores an existing movie to its prior Pool or Stash, or puts a
direct TMDB selection in its Adder's Stash. It closes only the local Acquisition
requirement. It does not delete, stop, or change Radarr data or work.

## Design
The full design system and the decisions behind it live in
[`docs/DESIGN.md`](./DESIGN.md). It is the source of truth for tokens, component
vocabulary, the hero static-layout contract, accessibility, copy, and
guardrails. Read it before any web/UI work.
