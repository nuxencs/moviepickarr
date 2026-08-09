# Frontend Performance Review — moviepickarr

Date: 2026-07-24
Scope: both tracks — live profile of the production build plus static review of the
`perf/virtualize-search-lists` branch (`git diff develop...HEAD`).
React Compiler is not enabled (no `babel-plugin-react-compiler`, no compiler config,
`eslint-plugin-react-hooks` v5), so re-render findings were reviewed normally.

## Live Metrics (Track A)

> Lab data from local runs of the **production build** (fresh `vite build` embedded
> into the Go binary, served same-origin at :3030 with the seeded fixture DB: 11
> pooled, 120 watched, 1590 actor options). Two trace runs, no CPU/network
> throttling — directional, not field data.

| Metric | Value | Threshold (good) | Status |
|---|---|---|---|
| LCP | 518–565 ms (2 runs) | ≤ 2.5s | Pass |
| CLS | 0.00 (both runs) | ≤ 0.1 | Pass |
| TBT (INP proxy) | ~0 ms (no long-task insight in either trace) | ≤ 200ms | Pass |
| TTFB | 1–2 ms | ≤ 0.8s | Pass |

CLS of 0.00 is worth calling out for this branch specifically: the virtualizers
start from height estimates (300px grid rows, 32px options) and re-measure real
elements, and that estimate-to-measured correction produced no observable layout
shift on load.

## Findings

### Critical

- None.

### Warning

- **[Re-renders]** `web/src/components/moviepickarr/MoviesTab.tsx:59-62` — the
  watched search state (`search` + `useDeferredValue`) lives on `MoviesTab`, so
  every keystroke re-renders the entire tab twice (the urgent pass and the
  deferred pass), including the unrelated "In the Pool" rail
  (`MoviesTab.tsx:101-155`). Measured on the dev server: of the ~84 component
  renders per steady-state keystroke, ~46 are the pool rail (22 `AdderTag` +
  24 `Avatar` = 11 pool tiles × 2 passes) and only ~2 are the watched list
  itself. The cost is bounded (the pool is capped) and typing stays flat, so
  this doesn't undo the branch's win — it's the remaining fixed overhead. Fix:
  move the search state down into a `WatchedSection` component that owns the
  input, count, and `VirtualWatched` (preferred), or wrap the pool rail in
  `React.memo`. Either cuts the per-keystroke render count roughly 10×.

  **Fixed on this branch.** `WatchedSection` was extracted; a steady-state
  keystroke now costs 2 commits and 30 component renders (down from ~84), and
  neither `MoviesTab` nor the pool tiles appear in the tally. What is left is
  the section's own chrome plus the two `NumberFlow` counts.

### Info

- **[Effects]** `web/src/hooks/useGridMetrics.ts:78-85` — `read()` does a
  `getComputedStyle` + `getBoundingClientRect` (forced style/layout read) inside
  a `ResizeObserver` that observes both the grid and `document.body`. The body
  observer fires whenever total page height changes, including when the
  virtualizer's own measurements resize the container, so each such change costs
  one synchronous layout read. The `same()` guard correctly prevents update
  loops, and no reflow cost from this hook showed up in the traces (the
  virtualizer's `initialOffset` read totals 0.1 ms), so this is fine as shipped —
  just the place to look first if grid resize ever shows in a trace.
- **[Load]** `[ForcedReflow insight]` — both traces flag ~127 ms of forced
  reflow during initial load. The named call frames (`addNewAndUpdateExisting`,
  `didUpdate`) resolve to `number-flow` (the animated counters), which predates
  this branch; the only frame from the new code is tanstack virtual-core's
  `initialOffset` at 0.1 ms. Pre-existing, load-time-only, and outside this
  diff's scope — noted so it isn't attributed to the virtualization work.
- **[Re-renders]** `web/src/components/moviepickarr/FilterBar.tsx:126-149` — the
  focus-recovery effect re-runs a `querySelector` on every identity change of
  `rendered`, i.e. every time the virtual window moves while scrolling the menu.
  The dependency is deliberate (it's how a pending focus lands) and the work is
  one DOM query, so this is just a note; if menu scrolling ever needs tightening,
  gate the query on `pendingFocus.current || activeIndex !== -1`.
- **[Interplay, positive]** `web/src/index.css:707-856` — removing
  `content-visibility: auto` from the watched tiles alongside virtualization is
  correct, not just cleanup: `contain-intrinsic-size` would have fed the
  virtualizer's `measureElement` placeholder heights instead of real ones. The
  memoization added around the choice lists (`FilterBar.tsx`, `personChoices` /
  `genreChoices` / `ReleaseYearSelect`'s `useMemo`) is exactly what keeps each
  menu's `filterChoices` memo stable across unrelated parent renders — confirmed
  at runtime (see below).

## Runtime re-renders (Track A / DevTools commit hook)

Dev-server profiling (`bun run dev`, :5173) with the commit hook injected as an
initScript. One methodology note: the skill's stock hook walks the whole fiber
tree on flags alone, which counts stale `PerformedWork` flags on fully-bailed-out
subtrees (those fibers are never re-cloned, so the flag persists) — it reported
~500 renders/keystroke including the entire stats page. Re-measured with a
DevTools-style walker that only descends where the child pointer changed; the
numbers below are from the corrected walker, and the profiling skill's hook has
since been fixed the same way.

**Watched search, route `/` (120 movies):** typing "godfather" one keystroke at a
time —

| Typed | Commits | Component renders | Tiles in DOM |
|---|---|---|---|
| g | 14 | 516 (result set grows to 30 visible tiles, mounts + count animation) | 30 |
| go | 7 | 133 | 4 |
| god | 3 | 87 | 1 |
| godf → godfather (6 keystrokes) | 2 each | 84 each | 1 |
| cleared | 5 | 321 (remounts the visible window) | 30 |

Steady state is exactly 2 commits (urgent + deferred pass) and a flat ~84
renders per keystroke regardless of library size; the DOM never holds more than
~30 of the 120 tiles. The spikes are legitimate work (mounting newly matching
tiles, `NumberFlow` count animation commits). Of the 84, ~2 are the watched list
— the rest is the pool rail + tab chrome re-rendering (the Warning above).

**Actors filter menu search, route `/stats` (1590 options):** typing "anderson" —

| Typed | Commits | Component renders | Options in DOM |
|---|---|---|---|
| a → anderson (every keystroke) | 2 | 10 | 17 → 4 |

Perfectly flat: 2 commits and 10 component renders per keystroke (the
`FilterChipMenu` twice plus its 4 icons), with 17 of 1590 options in the DOM.
Menu-local state stays menu-local — the stats panels below never re-render on a
keystroke. This is the branch's headline behavior, confirmed.

Keyboard navigation (ArrowDown roving focus through the virtualized options)
also commits once and renders only the menu.

## Bundle notes

- Production build (from `/tmp` build log): `index` 405.38 kB (121.53 kB gzip),
  `tanstack` 118.11 kB (37.42 kB gzip), `react-vendor` 12.55 kB (4.38 kB gzip),
  CSS 74.35 kB (14.77 kB gzip). No chunk exceeds the ~200 kB-gzip line.
- New dependency: `@tanstack/react-virtual` ^3.14.8 (plus its `virtual-core`).
  Small (a few kB gzipped inside the index chunk), no overlap with existing
  deps, and it's the standard companion to the tanstack stack already in use.
  Justified.

## Summary

1. The branch does what it claims: per-keystroke work is flat and small in both
   virtualized lists (2 commits / 10 renders in the 1590-option Actors menu;
   2 commits / ~84 renders in the watched grid), the DOM holds only a viewport's
   worth of rows, and load vitals are green with CLS 0.00. No Critical findings.
2. (Warning) The one worthwhile follow-up: move the watched search state out of
   `MoviesTab` into a section component (or memo the pool rail) so a keystroke
   stops re-rendering the 11-tile pool rail twice — ~46 of the ~84 renders per
   keystroke are that rail.
3. (Info) `useGridMetrics`' body-wide `ResizeObserver` does a forced layout read
   per page-height change; currently invisible in traces, just the first suspect
   if grid resizing ever regresses.
4. (Info) The ~127 ms of load-time forced reflow in the traces is `number-flow`
   (pre-existing), not the virtualization; the new code contributes 0.1 ms.
