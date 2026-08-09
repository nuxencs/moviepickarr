# Frontend and Persistence Performance Review: moviepickarr

Date: 2026-07-30

Completed: 2026-08-01

Scope: live production profiling and static review of
`9f7d49885ad2af34e2c32002b27614bb9f187efd^..ea76fae21cb7be8d278ec6387634adfeb8e38b68`,
followed by the merged fix stack through pull request #295 and the open
follow-up stack at [#298](https://github.com/nuxencs/moviepickarr/pull/298),
[#299](https://github.com/nuxencs/moviepickarr/pull/299), and #301 through
[#305](https://github.com/nuxencs/moviepickarr/pull/305).

The first Lighthouse and React DevTools results below are the live snapshot
taken at pull request #278. Pull requests #279 through #295 were initially
assessed with static review, CI build output, database-operation counts, and
targeted Chromium layout measurements. A final residual pass then profiled the
complete #295 code on desktop and simulated mobile and benchmarked the backend
paths that had remained unmeasured. A second residual pass measured the
follow-up stack and three lab prototypes for Hero artwork, font delivery, and
route splitting.

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

## Full-route baseline at pull request #295

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

## Follow-up stack metrics at pull request #305

Matched Lighthouse runs compared the #304 parent with the #305 mobile Hero
reservation. Each retained row is the median of three cold-cache navigations.
The #295 CLS value is not a baseline for this comparison: #302 changed when
known Hero content commits while mobile still had no height reservation. The
#304 parent isolates that intervening behavior; TMDB and browser state also
make cross-session CLS values unsuitable as a trend line.

| Revision and preset | Score | Median LCP | Median CLS | Median TBT |
|---|---:|---:|---:|---:|
| #304, Movies simulated mobile | 68 | 3.760 s | 0.43742 | 3 ms |
| #305, Movies simulated mobile | 87 | 3.760 s | 0 | 1 ms |
| #304, Movies desktop | 100 | 0.822 s | 0.00116 | 0 ms |
| #305, Movies desktop | 100 | 0.818 s | 0.00122 | 0 ms |

[#302](https://github.com/nuxencs/moviepickarr/pull/302) makes known draw
content and controls independent of backdrop loading. #305 then reserves the
mobile Hero's pending geometry, removing the repeatable shift without changing
median mobile LCP. Single follow-up runs at 320 and 340 px recorded CLS of
0.00012 and 0.00026. Mobile LCP remains above the lab threshold.

The final source selection snapshot also verifies the #303 and #304 poster
changes. Counts reflect the images loaded in each viewport, not the full
fixture.

| Route and preset | Selected poster candidates | Poster resource bytes | Lighthouse estimated waste |
|---|---|---:|---:|
| Movies, simulated mobile | 9 at `w342` | 172,976 | 30,325 bytes |
| Movies, desktop | 12 at `w185`, 11 at `w342` | 356,436 | 103,425 bytes |
| Members, simulated mobile | 3 at `w185` | 20,360 | 0 bytes |
| Members, desktop | 18 at `w154` | 123,688 | 16,809 bytes |

The remaining Movies estimates include TMDB compression suggestions and tiles
that need `w342` at their rendered size and DPR. The Members desktop estimate
is compression-only; mobile no longer jumps from `w154` to `w342` for the
middle-size slots.

## Frontend findings

### Critical

None.

### Warning

| Area | Finding | Result |
|---|---|---|
| Image delivery | Members always fetched `w342` posters for desktop slots rendered at 96 to 126 px. Seventeen posters used 396,012 resource bytes and Lighthouse estimated 367,168 bytes of waste. | [#278](https://github.com/nuxencs/moviepickarr/pull/278) adds `w154` and `w342` candidates plus pool and stash size hints. Poster bytes fell to 116,232, down 70.7%, and estimated waste fell to 16,809 bytes, down 95.4%. |
| LCP variability | The first cold Movies run reached 2.55 s LCP. Its TMDB hero backdrop spent 2.08 s in resource-load delay while transfer took about 85 ms. The next two runs were 0.91 to 0.98 s. | #302 decouples known draw content from artwork and #305 removes the mobile geometry shift. The #305 mobile median remains 3.760 s, so field LCP is still required before more delivery work. |
| Narrow movie modal | At 320 and 340 px, the modal's inner scroller and action rail overflowed and clipped external links and Delete. | [#295](https://github.com/nuxencs/moviepickarr/pull/295) stacks the poster above a paired footer at 370 px and below. The change is CSS-only. |
| Hero readiness and layout | The known draw and its controls waited for a remote backdrop decode. Mobile removed the desktop height reservation, so committing the decoded Hero shifted the main content. | [#302](https://github.com/nuxencs/moviepickarr/pull/302) renders content independently, repaints same-draw artwork changes, and rejects stale decodes. [#305](https://github.com/nuxencs/moviepickarr/pull/305) reduces matched mobile CLS from 0.43742 to 0. Responsive backdrop sources did not improve LCP. |
| Movies image delivery | The final #295 desktop Movies run loaded 19 fixed `w342` posters. Lighthouse estimated 311,022 bytes of poster waste, including CDN compression suggestions. | [#303](https://github.com/nuxencs/moviepickarr/pull/303) adds `w185`; [#304](https://github.com/nuxencs/moviepickarr/pull/304) adds route-specific size contracts. The #305 snapshot still estimates 103,425 desktop bytes and 30,325 mobile bytes, partly from CDN compression. |
| Members mobile candidates | Three 104 CSS px mobile slots selected `w342` because their 181 physical px requirement fell between the available 154 and 342 px candidates. | #303 adds `w185`. The final mobile snapshot selects it for all three loaded posters and reports zero image-delivery waste. |
| Fonts | Google Fonts CSS took 810 to 843 ms in the mobile runs. Lighthouse estimated about 300 ms of possible FCP improvement. | A matched self-hosted prototype improved LCP but delayed FCP by 453 to 602 ms on mobile. The dependencies were removed and Google delivery remains. |

### Information

| Area | Finding | Result |
|---|---|---|
| Forced reflow | The Members rail ran its overflow layout read for every bubbled `transitionend`. | [#272](https://github.com/nuxencs/moviepickarr/pull/272) limits the read to the drawer's `grid-template-rows` transition. The final Members trace reports 0 ms in the forced-reflow insight. |
| Legacy image selection | The Safari 26 fallback uses conservative desktop stash sizes. At a few column-reset widths and high DPR it can select `w342` when `w154` would cover the rendered slot. | This does not regress the original fixed `w342` behavior. Exact fallback formulas would duplicate container, rail, gap, and root-zoom layout calculations in markup. |
| Other poster surfaces | Hero, modal, reel, search, and Stats call sites retain their existing image contracts. The Movies route still has measurable waste after the bounded grid change. | Change another surface only with its own rendered-size and DPR evidence. |
| Route bundle | Members loads the eager Movies landing-route code. Its final mobile unused-JavaScript audit estimated 71,186 bytes. | A measured split cut that estimate to 22,311 bytes but delayed Movies mobile FCP by 221 ms and LCP by 63 ms. The prototype was removed. |

## Rejected lab prototypes

These experiments used the same seeded production build, authenticated browser
profile, throttling, and three-run medians as the #305 follow-up. None remains
in the source tree or lockfile.

### Responsive Hero artwork

A detached responsive image reduced the mobile backdrop resource from 86,084
to about 40,500 bytes. With high fetch priority, the `w780` candidate changed
median mobile LCP from 3.760 to 3.975 s and desktop LCP from 0.822 to 0.948 s.
Automatic priority reached 3.834 s mobile and 0.825 s desktop. It also froze the
selected `currentSrc` after copying it into CSS, and a `100vw` size hint was
under-dense for the portrait cover geometry. The byte reduction did not produce
a repeatable timing gain, so the fixed `w1280` backdrop remains.

### Self-hosted variable fonts

The prototype used current Fontsource packages for Geist, Geist Mono, and Hubot
Sans. The build emitted fourteen subset WOFF2 assets; each retained navigation
requested three Latin files containing 100,812 resource bytes, 168 more than
the three Google font files, excluding Google CSS. The imports also added about
0.75 kB gzip of CSS.

| Route and preset | Parent FCP | Self-hosted FCP | Parent LCP | Self-hosted LCP |
|---|---:|---:|---:|---:|
| Movies, simulated mobile | 2.002 s | 2.604 s | 3.766 s | 3.355 s |
| Members, simulated mobile | 2.277 s | 2.730 s | 3.385 s | 3.197 s |
| Movies, desktop | 0.454 s | 0.522 s | 0.825 s | 0.748 s |
| Members, desktop | 0.528 s | 0.569 s | 0.797 s | 0.636 s |

`font-display: optional` variants did not change the request waterfall. The LCP
gain did not justify delaying first content on every measured route.

### Movies route split

The prototype kept Hero eager and deferred the board by one animation frame. It
reduced the entry chunk from 140.03/44.99 kB raw/gzip to 95.26/30.87 kB and
split TanStack Virtual into a 24.47/7.45 kB chunk.

| Route and preset | Parent FCP | Split FCP | Parent LCP | Split LCP | Parent unused JS | Split unused JS |
|---|---:|---:|---:|---:|---:|---:|
| Movies, simulated mobile | 2.002 s | 2.223 s | 3.766 s | 3.829 s | 21,350 B | 0 B |
| Members, simulated mobile | 2.277 s | 2.278 s | 3.385 s | 3.302 s | 71,186 B | 22,311 B |
| Movies, desktop | 0.454 s | 0.474 s | 0.825 s | 0.835 s | 21,322 B | 0 B |
| Members, desktop | 0.528 s | 0.509 s | 0.797 s | 0.819 s | 71,057 B | 22,189 B |

The Members mobile profile used the otherwise identical immediate-split build;
the final one-frame change only scheduled the Movies board and changed the
entry chunk by 0.04 kB.

The direct Members route benefits, but the default Movies route regressed on
mobile and did not gain on desktop. No route traffic data supports making that
trade, so the eager route remains.

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

## Later-stack runtime and persistence costs

| Pull requests | Change | Performance effect |
|---|---|---|
| [#279](https://github.com/nuxencs/moviepickarr/pull/279), [#281](https://github.com/nuxencs/moviepickarr/pull/281) | Held-winner comparison and publication-generation guards. | Constant-time state checks and timer bookkeeping; no material cost. |
| [#280](https://github.com/nuxencs/moviepickarr/pull/280), [#282](https://github.com/nuxencs/moviepickarr/pull/282), [#291](https://github.com/nuxencs/moviepickarr/pull/291) | Serialize conflicting draw commands, watch rotation, pool membership, and lock writes. | Narrow locks trade possible contention for ordered state transitions. Parsing and response encoding remain outside, but next-up authorization, draw metadata and publication, and the move response projection and publication stay inside their respective boundaries. Focused benchmarks bound the cost below. |
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
| [#298](https://github.com/nuxencs/moviepickarr/pull/298) | Invalidate next-up after member creation, restoration, archive, or deletion. | Two constant query-key additions on infrequent roster events; no new request outside those invalidations. |
| [#299](https://github.com/nuxencs/moviepickarr/pull/299) | Publish a committed move without projecting an unused response. | Removes the owner lookup plus full pool, stash, and metadata projection from the serialized move path. The endpoint now returns `204`; clients already refetch after the event. |
| [#301](https://github.com/nuxencs/moviepickarr/pull/301) | Reject local wall times that normalize across a daylight-saving gap. | One local round trip through the existing formatter during form submission; no render or network cost. |
| [#302](https://github.com/nuxencs/moviepickarr/pull/302) | Render known draw content before backdrop decode. | Keeps one detached image preload and avoids blocking controls or text on its promise. No extra image request appears in the measured trace. |
| [#303](https://github.com/nuxencs/moviepickarr/pull/303), [#304](https://github.com/nuxencs/moviepickarr/pull/304) | Add a middle poster candidate and Movies grid size contracts. | Browser-native source selection; no viewport reads or render work. Loaded bytes depend on tile size and DPR. |
| [#305](https://github.com/nuxencs/moviepickarr/pull/305) | Reserve the pending mobile Hero body. | CSS-only. Matched mobile CLS falls from 0.43742 to 0 with unchanged median LCP. |

Issue [#157](https://github.com/nuxencs/moviepickarr/issues/157) is closed by
the merged #282, #283, and #285 transaction work. #299 addresses a separate
move-publication gap.

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

The move benchmark predates #299. That pull request removes the complete owner
pool and stash reload plus metadata projection from `poolStateMu`, returns an
empty success response, and publishes directly after a changed commit. The
fixed path was not rebenchmarked, so the 0.251 and 14.484 ms values above are
historical measurements rather than final-stack results. They likely overstate
the fixed path because the measured projection was removed.

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

The #305 Web CI build reports `index` at 140.03/44.99 kB raw/gzip, main CSS at
63.49/13.14 kB, `react-vendor` at 185.33/58.41 kB, and `tanstack` at
142.92/44.47 kB. Relative to #295, the follow-up stack adds 1.56 kB raw and
0.49 kB gzip to `index`, plus 0.05 kB raw CSS with no rounded gzip increase.

No chunk exceeds 200 kB gzip. Route-level lazy imports remain in place. Neither
fix stack adds a dependency. The original range updates `@number-flow/react`
from 0.6.0 to 0.6.2 and `eslint-plugin-react-refresh` from 0.4.26 to 0.5.3.
Static review found no whole-library imports, duplicate utility libraries,
unstable production list keys, or inline context values in the reviewed range.

## Verification snapshot

- Pull request #295 Web CI passed 39 test files and 553 tests, then completed
  the production build in 3.37 seconds.
- Pull request #305 Web CI passed 41 test files and 563 tests, then completed
  the production build in 3.18 seconds.
- The 32-pull-request original stack is merged. All seven checks pass on each
  open follow-up pull request at #298, #299, and #301 through #305.
- The narrow modal was measured at 320, 340, 370, and 371 px in an authenticated
  Chromium session after a production build.
- Twelve cold-cache Lighthouse runs covered Movies and Members on desktop and
  simulated mobile against the final #295 code.
- Matched three-run profiles covered the #304 and #305 Movies behavior, both
  routes and presets for the font prototype, and both routes and presets for
  the route-split prototype. Additional runs covered responsive Hero priority
  and 320/340 px layout.
- Focused serialization race tests passed 10 times. Authentication, migration,
  and import tests passed against the benchmarked code.
- Each follow-up logic or layout fix began with a failing focused regression
  test. Lab-only prototypes were removed after their comparisons.

## Measurement limits

- Lighthouse navigation data is not field data. TBT of 0 to 26 ms is only an
  INP proxy because no interaction occurred; deployed real-user monitoring is
  required for field INP.
- Critical-section benchmarks are sequential service measurements, not
  concurrent HTTP tail latency. They use warm local storage and synthetic data.
- Local loopback removes production proxy, geographic TTFB, storage, and
  deployment effects. TMDB latency and cache state remain external variables.
- The current draw loaded directly. The reel, confirmation, and watch
  interaction were not profiled as a Lighthouse user flow. Responsive Hero
  source selection was observed for one loaded draw, not repeated rotation.
- Lighthouse 13 image estimates combine resizing with compression suggestions
  controlled by TMDB's CDN. No live Safari profile was run.
- Migration timing excludes the production integrity check and backup copy.
  Import timing starts from an existing Bolt source.
- The matched #304/#305 CLS comparison covers Movies only. The 320 and 340 px
  checks are single runs. The CSS reservation is a minimum geometry guarantee,
  not a maximum for unusually long translated content.
- Font and route-split results are local navigation data. They do not include
  repeat visits, field cache behavior, route traffic share, or user flows that
  enter Members before Movies.
- Runtime rerender profiling was performed at #278 and was not repeated after
  the CSS and image-source follow-ups.
- The extra invalidation traffic from #298 was not measured during a burst of
  roster events.
- The move critical-section benchmark was not rerun after #299 removed the
  scaling response projection.

## Summary

The cumulative work has no material range-specific performance regression. The
original desktop profile remains 99 on both routes. The follow-up stack removes
the known-draw artwork dependency, takes matched mobile Hero CLS from 0.43742 to
0, fills the missing poster candidate, sizes the Movies grid, and removes the
unused move projection from its serialized path. The Members poster fix retains
its measured 70.7% byte reduction.

Mobile lab LCP remains about 3.76 s on Movies and above 3 s on Members. Smaller
Hero artwork, self-hosted fonts, and a Movies route split each failed their
matched tradeoff, so none was kept. Remaining work requires field route,
interaction, cache, and deployment evidence rather than another lab-only code
change. Safari fallback selection and TMDB compression estimates remain known
measurement limits.
