# Frontend and Persistence Performance Review: moviepickarr

Date: 2026-07-30

Completed: 2026-08-01

Scope: live production profiling and static review of
`9f7d49885ad2af34e2c32002b27614bb9f187efd^..ea76fae21cb7be8d278ec6387634adfeb8e38b68`,
followed by the stacked fixes through pull request #295,
`fix/narrow-movie-modal-rail`.

The first Lighthouse and React DevTools results below are the live snapshot
taken at pull request #278. Pull requests #279 through #295 were initially
assessed with static review, CI build output, database-operation counts, and
targeted Chromium layout measurements. A final residual pass then profiled the
complete #295 code on desktop and simulated mobile and benchmarked the backend
paths that had remained unmeasured.

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

## Final live metrics at pull request #295

The final profile used a fresh production build, an authenticated fixture with
7 members and 206 movies, Lighthouse 13.4.1, and a dedicated Chromium profile.
The browser cache was cleared before every retained navigation. Three runs were
recorded for each route and preset.

| Route and preset | Score | Median LCP | Median CLS | Median TBT | LCP range |
|---|---:|---:|---:|---:|---:|
| Movies, desktop | 99 | 0.847 s | 0.00111 | 0 ms | 0.836 to 0.866 s |
| Members, desktop | 99 | 0.784 s | 0.00056 | 0 ms | 0.781 to 0.789 s |
| Movies, simulated mobile | 86 | 3.752 s | 0.08308 | 19 ms | 3.680 to 3.753 s |
| Members, simulated mobile | 88 | 3.688 s | 0.00002 | 0 ms | 3.679 to 3.697 s |

The mobile preset used a 412 by 823 viewport at 1.75 DPR, four-times CPU
slowdown, 150 ms RTT, and 1.64 Mbps throughput. Both mobile routes missed the
2.5 s lab LCP threshold in every run. This is a repeatable lab risk, not a field
Core Web Vitals result.

## Frontend findings

### Critical

None.

### Warning

| Area | Finding | Result |
|---|---|---|
| Image delivery | Members always fetched `w342` posters for desktop slots rendered at 96 to 126 px. Seventeen posters used 396,012 resource bytes and Lighthouse estimated 367,168 bytes of waste. | [#278](https://github.com/nuxencs/moviepickarr/pull/278) adds `w154` and `w342` candidates plus pool and stash size hints. Poster bytes fell to 116,232, down 70.7%, and estimated waste fell to 16,809 bytes, down 95.4%. |
| LCP variability | The first cold Movies run reached 2.55 s LCP. Its TMDB hero backdrop spent 2.08 s in resource-load delay while transfer took about 85 ms. The next two runs were 0.91 to 0.98 s. | The final mobile profile made the risk repeatable in lab conditions. Hero discovery, sizing, and readiness are now bounded follow-up work; field data is still required. |
| Narrow movie modal | At 320 and 340 px, the modal's inner scroller and action rail overflowed and clipped external links and Delete. | [#295](https://github.com/nuxencs/moviepickarr/pull/295) stacks the poster above a paired footer at 370 px and below. The change is CSS-only. |
| Hero readiness and layout | The known draw and its controls wait for a remote backdrop decode. Mobile also removes the desktop height reservation, so committing the decoded hero shifted the main content by 0.08197 CLS. | Both paths predate the range. Separate content readiness from artwork, reserve mobile geometry, then measure responsive backdrop candidates and priority. |
| Movies image delivery | The final desktop Movies run loaded 19 fixed `w342` posters. Lighthouse estimated 311,022 bytes of poster waste, including CDN compression suggestions. | Add `w185` and route-specific size hints one bounded surface at a time. Several mobile tiles still require `w342` at high DPR. |
| Members mobile candidates | The #278 desktop result persisted, but three 104 CSS px mobile slots selected `w342` because their 181 physical px requirement falls between the available 154 and 342 px candidates. | Add a `w185` candidate; the existing size formulas are accurate. |
| Fonts | Google Fonts CSS took 810 to 843 ms in the mobile runs. Lighthouse estimated about 300 ms of possible FCP improvement. | `display=swap` still passes. Compare self-hosted variable fonts and fallback metrics before changing delivery. |

### Information

| Area | Finding | Result |
|---|---|---|
| Forced reflow | The Members rail ran its overflow layout read for every bubbled `transitionend`. | [#272](https://github.com/nuxencs/moviepickarr/pull/272) limits the read to the drawer's `grid-template-rows` transition. The final Members trace reports 0 ms in the forced-reflow insight. |
| Legacy image selection | The Safari 26 fallback uses conservative desktop stash sizes. At a few column-reset widths and high DPR it can select `w342` when `w154` would cover the rendered slot. | This does not regress the original fixed `w342` behavior. Exact fallback formulas would duplicate container, rail, gap, and root-zoom layout calculations in markup. |
| Other poster surfaces | Default `Poster` output stays at `w342` without responsive attributes. The main Movies route still shows pre-existing image-delivery waste. | Opt other bounded surfaces into the source contract one at a time and measure each route. |
| Route bundle | Members loads the eager Movies landing-route code. Its mobile unused-JavaScript audit estimated 70,686 bytes and 450 ms of LCP opportunity. | A Movies route split helps direct visits to other routes but adds a request to the landing page. Measure both paths before changing the router. |

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
| [#280](https://github.com/nuxencs/moviepickarr/pull/280), [#282](https://github.com/nuxencs/moviepickarr/pull/282), [#291](https://github.com/nuxencs/moviepickarr/pull/291) | Serialize conflicting movie-night commands, watch rotation, pool membership, and lock writes. | Narrow locks trade possible contention for ordered state transitions. Parsing and response encoding remain outside, but next-up authorization, draw metadata and publication, and the move response projection and publication stay inside their respective boundaries. Focused benchmarks bound the cost below. |
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

## Residual backend benchmarks

Benchmarks ran against disposable WAL databases on an Apple M4 Pro using the
actual repositories and migrations. Critical-section values are medians from
10 runs of 20 operations per fixture.

| Path | Representative fixture | Median | Synthetic extreme | Median |
|---|---:|---:|---:|---:|
| Draw | 3 candidates | 0.116 ms | 3,000 candidates | 6.902 ms |
| Watch and rotate | 100 active members | 0.156 ms | 10,000 members | 4.927 ms |
| Move | 100 owner movies | 0.251 ms | 5,000 movies | 14.484 ms |
| Pool-lock toggle | fixed work | 0.013 ms | fixed work | 0.013 ms |

Pure mutex cost was about 100 ns per operation at eight-way contention.
Synchronous broker fanout took 19.611 microseconds at 1,000 ready subscribers.
The meaningful scaling comes from work held inside the boundary, not the lock
or broker.

The move path is the clearest remaining target. It holds `poolStateMu` through
a complete owner pool and stash reload plus metadata projection. The frontend
does not consume the HTTP response, and SSE consumers use `movie:moved` only as
an invalidation. Removing or moving that projection would also close a
pre-existing publication gap when a post-commit read fails.

Indexed active-user guards added 3.2 to 3.6 microseconds to affected local and
OIDC operations. The session query showed no regression because it already
joined `users`. Combined local read and write guard work was about 0.043% of the
15.59 ms median Argon2 verification time.

Migration 010 completed 100,000 movies in 169.64 ms when clean and 213.10 ms
with 10% identity conflicts. Bolt-to-SQLite import completed 50,000 movies in
1.877 s when clean and 1.917 s with 10% conflicts. These startup-only costs do
not warrant a change.

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
- Twelve cold-cache Lighthouse runs covered Movies and Members on desktop and
  simulated mobile against the final #295 code.
- Focused serialization race tests passed 10 times. Authentication, migration,
  and import tests passed against the benchmarked code.

## Measurement limits

- Lighthouse navigation data is not field data. TBT of 0 to 26 ms is only an
  INP proxy because no interaction occurred; deployed real-user monitoring is
  required for field INP.
- Critical-section benchmarks are sequential service measurements, not
  concurrent HTTP tail latency. They use warm local storage and synthetic data.
- Local loopback removes production proxy, geographic TTFB, storage, and
  deployment effects. TMDB latency and cache state remain external variables.
- The current draw loaded directly. The reel, confirmation, and watch
  interaction were not profiled as a Lighthouse user flow.
- Lighthouse 13 image estimates combine resizing with compression suggestions
  controlled by TMDB's CDN. No live Safari profile was run.
- Migration timing excludes the production integrity check and backup copy.
  Import timing starts from an existing Bolt source.

## Summary

The cumulative stack has no material range-specific performance regression.
The final desktop profile remains 99 on both routes, the Members poster fix
retains its 70.7% byte reduction, transition-driven layout reads were removed,
and the #295 layout adds only 0.03 kB gzip of CSS. Backend serialization and
large-library costs are small at representative sizes and bounded at synthetic
extremes.

The residual work predates the reviewed range: mobile LCP and hero layout,
fixed Movies posters, a missing middle image candidate, external font delivery,
and an unused move response held inside serialization. Field INP and deployment
latency remain the important measurement gaps.
