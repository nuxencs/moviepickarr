# moviepickarr: Product

Register: product. An app/tool, so design serves the task and earned
familiarity beats novelty.

## What it is
A small private web app for a friend group to run a shared
movie-night rotation. Each member keeps a personal stash of movies and
promotes up to 3 into a shared pool. The app picks a random movie from the
pool for movie night, then tracks the watched history and stats. Movies are
enriched with TMDB metadata (poster, backdrop, runtime, rating, genres, tagline,
overview, external links, and cast/crew credits).

## Audience
A handful of friends who know each other. Private, authenticated by obscurity,
not a public or marketing surface. Desktop-first, with a dedicated mobile/touch
pass on top (bottom tab bar, touch-reachable actions; see DESIGN.md §13).

## Primary surfaces (tabs)
- Movies: the cinematic hero (current pick, actions, next picker), the pool
  grid, and the watched library (grid/list, searchable by title or picker). The
  Movies page stays browse-first; metadata drill-downs live on Stats. A movie's
  detail modal shows "Directed by / Written by" credit lines and a horizontally
  scrollable cast strip (TMDB headshots, billing order); each cast card links to
  the person's TMDB page.
- Users: members, each with their pool slots (max 3) and a searchable stash;
  add via TMDB search.
- Stats: watch stats over a time window (member leaderboard where every member
  always has a row, zero or not, whatever the window or filters; weekday and
  hourly activity; custom date range), plus a TMDB deep-dive over the watched
  subset: hours-watched and average-rating KPIs (with an average-runtime
  sub-line), a horizontally-scrolling rail of the actual films behind the count
  (click a poster for its detail modal), a top-genres donut, most-watched
  director and actor rails, and a release-decades timeline. One filter system
  sits under the header: the time presets, the watch-year quick-select (snaps
  the custom window to a calendar year), genre, release year or whole decade
  (mutually exclusive), "Picked by" (the movie's picker), and multi-select
  Actors / Crew people filters. People filters are any-of within a list and
  AND across filters; actors match cast credits, crew matches any whitelisted
  crew job (so filtering by a director also counts their writer credits).
  Clicking a person card on a rail toggles them in the matching people filter
  and drills the whole page down (every aggregate is computed server-side over
  the filtered subset); each card also carries a corner link to the person's
  TMDB page.

## Design
The full design system and the decisions behind it live in
[`docs/DESIGN.md`](./DESIGN.md). It is the source of truth for tokens, component
vocabulary, the hero static-layout contract, accessibility, copy, and
guardrails. Read it before any web/UI work.
