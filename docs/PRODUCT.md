# Movie Gang — Product

**Register: product** (an app/tool; design serves the task, earned familiarity over novelty).

## What it is
A small private web app for a friend group ("the gang") to run a shared
movie-night rotation. Each member keeps a personal **stash** of movies and
promotes up to **3** into a shared **pool**. The app picks a random movie from the
pool for movie night, then tracks the **watched** history and **stats**. Movies are
enriched with TMDB metadata (poster, backdrop, runtime, rating, genres, tagline,
overview, external links).

## Audience
A handful of friends who know each other. Private, authenticated-by-obscurity,
not a public or marketing surface. Desktop-first; mobile polish is a later pass.

## Primary surfaces (tabs)
- **Movies** — the cinematic hero (current pick + actions + next picker), the pool
  grid, and the watched library (grid/list, searchable).
- **Users** — members, each with their pool slots (max 3) and a searchable stash;
  add via TMDB search.
- **Stats** — watch stats over a time window (picker leaderboard, weekday and hourly
  activity, custom date range).

## Design
The full design system and the decisions behind it live in
[`docs/DESIGN.md`](./DESIGN.md). It is the source of truth for tokens, component
vocabulary, the hero static-layout contract, accessibility, copy, and guardrails.
Read it before any web/UI work.
