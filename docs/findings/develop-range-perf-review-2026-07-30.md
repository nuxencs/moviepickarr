# Frontend and Persistence Performance Review: moviepickarr

Date: 2026-07-30

Completed: 2026-07-31

Scope: live production profiling and static review of
`9f7d49885ad2af34e2c32002b27614bb9f187efd^..ea76fae21cb7be8d278ec6387634adfeb8e38b68`,
followed by the stacked fixes through pull request #295,
`fix/narrow-movie-modal-rail`.

The Lighthouse and React DevTools results below are the live snapshot taken at
pull request #278. Pull requests #279 through #295 were assessed with static
review, CI build output, database-operation counts from the changed paths, and
targeted Chromium layout measurements. They were not given a second full
Lighthouse profile.

React Compiler is not enabled. There is no compiler dependency or config, and
the project uses `eslint-plugin-react-hooks` v5, so memoization findings were
reviewed normally.

## Live metrics at pull request #278

Lab data came from local runs of the production build embedded in the Go binary
and served same-origin on port 3030 with the seeded fixture database. Lighthouse
used its desktop preset. These results are directional, not field data.

| Metric | Value | Good threshold | Result |
|---|---|---|---|
| LCP | Movies: 0.91 to 0.98 s steady, one 2.55 s cold outlier. Members after fix: 0.85 to 0.89 s. | 2.5 s or less | Pass except the noted cold outlier |
| CLS | Movies: 0.00092. Members: 0.00056 to 0.00070. | 0.1 or less | Pass |
| TBT, used as an INP proxy | Movies: 0 to 21 ms. Members: 18 to 24 ms. | 200 ms or less | Pass |
| TTFB | 0 to 1 ms | 0.8 s or less | Pass |

The final Members run scored 99 with 872 ms LCP, 24 ms TBT, 0.00056 CLS, and
0 ms TTFB.

## Frontend findings

### Critical

None.

### Warning

| Area | Finding | Result |
|---|---|---|
| Image delivery | Members always fetched `w342` posters for desktop slots rendered at 96 to 126 px. Seventeen posters used 396,012 resource bytes and Lighthouse estimated 367,168 bytes of waste. | [#278](https://github.com/nuxencs/moviepickarr/pull/278) adds `w154` and `w342` candidates plus pool and stash size hints. Poster bytes fell to 116,232, down 70.7%, and estimated waste fell to 16,809 bytes, down 95.4%. |
| LCP variability | The first cold Movies run reached 2.55 s LCP. Its TMDB hero backdrop spent 2.08 s in resource-load delay while transfer took about 85 ms. The next two runs were 0.91 to 0.98 s. | This predates the reviewed range. Field data should decide whether the hero moves to a cache-controlled origin or a preloadable responsive image. |
| Narrow movie modal | At 320 and 340 px, the modal's inner scroller and action rail overflowed and clipped external links and Delete. | [#295](https://github.com/nuxencs/moviepickarr/pull/295) stacks the poster above a paired footer at 370 px and below. The change is CSS-only. |

### Information

| Area | Finding | Result |
|---|---|---|
| Forced reflow | The Members rail ran its overflow layout read for every bubbled `transitionend`. | [#272](https://github.com/nuxencs/moviepickarr/pull/272) limits the read to the drawer's `grid-template-rows` transition. The final Members trace reports 0 ms in the forced-reflow insight. |
| Legacy image selection | The Safari 26 fallback uses conservative desktop stash sizes. At a few column-reset widths and high DPR it can select `w342` when `w154` would cover the rendered slot. | This does not regress the original fixed `w342` behavior. Exact fallback formulas would duplicate container, rail, gap, and root-zoom layout calculations in markup. |
| Other poster surfaces | Default `Poster` output stays at `w342` without responsive attributes. The main Movies route still shows pre-existing image-delivery waste. | Opt other bounded surfaces into the source contract one at a time and measure each route. |
| Fonts | Google Fonts CSS is render-blocking. The document already preconnects both origins, requests `display=swap`, and passes the font-display audit. | Self-host only if field data shows font latency. This was not introduced by the range. |

## Runtime re-renders at pull request #278

- Stash search for `the`: two commits, six `StashTile` renders, and six
  `PosterButton` renders. The rail and page shell did not rerender.
- Member switch: seven expected `RailRow` renders and fifteen destination
  `StashTile` renders. No unrelated route work appeared.
- Incremental watched search: two commits per settled key, with no
  `MoviesTab` or pool rerender.
- React Compiler is not enabled.

## Narrow-modal measurements

Targeted Chromium measurements used the enriched fixture. Before #295, the
inner scroller and body overflowed by 27 px at a 320 px viewport and 7 px at a
340 px viewport. The rail itself overflowed by 47 px and 27 px, respectively.
The capped modal did not widen; its inner scroller owned the overflow and
clipped the rail.

After #295, every measured modal, scroller, body, and rail had equal client and
scroll widths at both viewports. At 370 px the footer uses the stacked layout;
at 371 px the original row fits. The implementation adds no viewport read,
JavaScript branch, or render work.

## Later-stack persistence and concurrency costs

| Pull requests | Change | Performance effect |
|---|---|---|
| [#279](https://github.com/nuxencs/moviepickarr/pull/279), [#281](https://github.com/nuxencs/moviepickarr/pull/281) | Held-winner comparison and publication-generation guards. | Constant-time state checks and timer bookkeeping; no material cost. |
| [#280](https://github.com/nuxencs/moviepickarr/pull/280), [#282](https://github.com/nuxencs/moviepickarr/pull/282), [#291](https://github.com/nuxencs/moviepickarr/pull/291) | Serialize conflicting movie-night commands, watch rotation, pool membership, and lock writes. | Narrow locks trade possible contention for ordered state transitions. Authentication, parsing, response encoding, reads, and nonconflicting work remain outside the critical sections. No load benchmark was run. |
| [#283](https://github.com/nuxencs/moviepickarr/pull/283) | Insert movie identity and project the response in one transaction. | Removes one database write. |
| [#284](https://github.com/nuxencs/moviepickarr/pull/284) | Require an active user on authentication paths. | Adds one indexed user primary-key check to hot authentication reads. |
| [#285](https://github.com/nuxencs/moviepickarr/pull/285) | Seed the admin in a two-pass startup transaction. | Argon2 stays outside SQLite's writer, avoiding a long global write lock. The path is startup-only. |
| [#286](https://github.com/nuxencs/moviepickarr/pull/286) | Commit each movie edit as one writer transaction. | Changed-identity edits fall from about seven statements and four commits to four statements and one commit. Same-identity edits fall from four statements to three. |
| [#287](https://github.com/nuxencs/moviepickarr/pull/287) | Apply identity-matched enrichment atomically and coalesce retries. | Normal enrichment falls from three commits to one and removes one statement. Queue yielding prevents one movie from starving others. |
| [#288](https://github.com/nuxencs/moviepickarr/pull/288) | Clear derived data after an identity change. | Changed identities add one indexed cleanup statement. Same-identity edits add none. |
| [#289](https://github.com/nuxencs/moviepickarr/pull/289) | Canonicalize and enforce unique IMDb identities. | IMDb paths add one indexed uniqueness probe. Migration and import do a one-time `O(n log n)` sort and `O(n)` ownership pass. |
| [#290](https://github.com/nuxencs/moviepickarr/pull/290) | Return the exact detached draw snapshot from the service. | Draw repository calls fall from seven to four and redundant `ORDER BY RANDOM()` work is removed. |
| [#292](https://github.com/nuxencs/moviepickarr/pull/292) | Parse exact IMDb and TMDB edit URLs locally. | Bounded URL and regular-expression work; no network call or added SQL for valid unchanged edits. |
| [#293](https://github.com/nuxencs/moviepickarr/pull/293) | Name dialogs and restore focus after detached openers. | The DOM fallback scan runs only when the opener disappeared. Normal close stays on the direct focus path. |
| [#294](https://github.com/nuxencs/moviepickarr/pull/294) | Compare watched input to the stored minute before conversion. | Memoized string and date comparisons; unchanged timestamps omit the database assignment. |
| [#295](https://github.com/nuxencs/moviepickarr/pull/295) | Reflow the narrow movie-modal rail. | CSS-only; JavaScript output is unchanged from #294. CSS grows by 0.03 kB gzip. |

## Bundle notes

Pull request #295 Web CI produced these chunks:

| Chunk | Raw | Gzip |
|---|---:|---:|
| `react-vendor` | 185.33 kB | 58.41 kB |
| `tanstack` | 142.92 kB | 44.47 kB |
| `index` | 138.47 kB | 44.50 kB |
| `StatsPage` | 25.53 kB | 8.69 kB |
| `AdminPage` | 18.78 kB | 6.42 kB |
| `UsersPage` | 17.66 kB | 6.17 kB |
| Main CSS | 63.44 kB | 13.14 kB |

In the same CI environment, #278 to #295 changes the app `index` chunk from
136.41/43.66 kB to 138.47/44.50 kB, an increase of 2.06 kB raw and 0.84 kB
gzip. Main CSS changes from 62.49/13.01 kB to 63.44/13.14 kB, an increase of
0.95 kB raw and 0.13 kB gzip. `AdminPage` adds 0.04 kB gzip, `UsersPage` is
flat after rounding, and the Stats and vendor raw sizes are unchanged.

No chunk exceeds 200 kB gzip. Route-level lazy imports remain in place. The
range adds no dependency. It updates `@number-flow/react` from 0.6.0 to 0.6.2
and `eslint-plugin-react-refresh` from 0.4.26 to 0.5.3. Static review found no
whole-library imports, duplicate utility libraries, unstable production list
keys, or inline context values in the reviewed range.

## Verification snapshot

- Pull request #295 Web CI passed 39 test files and 553 tests, then completed
  the production build in 3.37 seconds.
- All seven pull request #295 checks passed. All 32 pull requests had completed,
  successful checks at handoff.
- The narrow modal was measured at 320, 340, 370, and 371 px in an authenticated
  Chromium session after a production build.

## Measurement limits

- The new command serialization and authentication paths have no load test.
- The identity migration and import path have no large-library benchmark.
- Lighthouse used its desktop preset and stopped at #278. There is no mobile
  Lighthouse result, field INP data, or full #295 re-profile.
- Remote TMDB latency can dominate cold hero LCP independently of local code.

## Summary

The cumulative stack has no material frontend performance regression. The only
large range-specific network waste fell by 70.7%, transition-driven layout
reads were removed, and the #295 narrow layout adds only 0.03 kB gzip of CSS.
Later backend fixes generally reduce statements, commits, or repository calls.
The main unmeasured tradeoff is contention at the newly serialized correctness
boundaries.
