# Develop Range Review: 2026-07-30

Completed: 2026-08-01

Scope: every change on `develop` from
`9f7d49885ad2af34e2c32002b27614bb9f187efd` through
`ea76fae21cb7be8d278ec6387634adfeb8e38b68`, including the first commit.

The reviewed range contains 19 commits across 49 files, with 5,642 additions
and 519 deletions. Most risk sat in the new Members surface, movie-modal
history, draw lifecycle state shared by the server and browser, and
multi-statement persistence flows.

## Outcome

No blocking finding remains in the proposed stack through pull request #295.
The review produced 32 stacked pull requests and 35 commits. These changes are
still open, so `develop` retains the original behavior until the stack merges.

The highest-impact findings were unrevealed winner leaks, commands that could
publish lifecycle events out of order, writes that could partially commit, and
stale enrichment that could overwrite a newer movie identity. Each fix is a
separate pull request except where one state machine owned both symptoms.
Regression tests were written first where the behavior fit automated testing.

A follow-up audit closed the original measurement gaps and found four defects
that predate the reviewed range. They do not block this stack. They are recorded
below for a separate TDD stack after merge.

## Fixed findings

### Draw and lifecycle ordering

| Impact | Finding | Fix |
|---|---|---|
| High | Movie detail exposed the database `current` status before the draw was revealed, while pool projections still hid it. | [#262](https://github.com/nuxencs/moviepickarr/pull/262) |
| Medium | A delayed pool-lock event could overwrite newer draw lifecycle state in the client cache. | [#273](https://github.com/nuxencs/moviepickarr/pull/273) |
| High | Promote, demote, and delete checked draw state before mutation, leaving a window where a concurrent draw could change the winner set or pool capacity. | [#274](https://github.com/nuxencs/moviepickarr/pull/274) |
| High | Moving an unrevealed held winner to the projected pool returned an invalid-state error that distinguished it from ordinary pooled movies. | [#279](https://github.com/nuxencs/moviepickarr/pull/279) |
| High | Watch persisted before turn rotation and publication, so the outgoing member could draw or reveal again and lifecycle events could invert. | [#280](https://github.com/nuxencs/moviepickarr/pull/280) |
| Medium | Auto-reveal could publish before `movie:drawn`, causing clients to ignore the reveal and retain a stale reel. | [#281](https://github.com/nuxencs/moviepickarr/pull/281) |
| High | Watch committed before turn rotation. A rotation failure returned success, left a stuck holder, and could not be repaired by retry. This is one of the flows in issue #157. | [#282](https://github.com/nuxencs/moviepickarr/pull/282) |
| High | Reel candidates were rebuilt after leaving the service lock, so the winner and the animated reel could represent different pool snapshots. | [#290](https://github.com/nuxencs/moviepickarr/pull/290) |
| High | Pool membership and lock writes could commit in a different order from admission, producing durable state and events in conflicting orders. | [#291](https://github.com/nuxencs/moviepickarr/pull/291) |

### Persistence, authentication, and identity

| Impact | Finding | Fix |
|---|---|---|
| High | Movie creation could leave an identityless orphan or commit a movie without its event and enrichment job. This is one of the flows in issue #157. | [#283](https://github.com/nuxencs/moviepickarr/pull/283) |
| High, security | Archived members could still authenticate through residual local, OIDC, session, invite, or seed data. | [#284](https://github.com/nuxencs/moviepickarr/pull/284) |
| Medium | A failed admin seed could leave a credentialless admin or an unintended promotion. This completes the three concrete flows in issue #157. | [#285](https://github.com/nuxencs/moviepickarr/pull/285) |
| High | Movie edits could partially commit, interleave into a value submitted by neither request, or leave watched-title stats stale. | [#286](https://github.com/nuxencs/moviepickarr/pull/286) |
| High | A stale enrichment response could overwrite a newer identity, while separate metadata and credit commits could leave mixed-film data. | [#287](https://github.com/nuxencs/moviepickarr/pull/287) |
| Medium | Changing movie identity could indefinitely expose the previous film's poster, cast, runtime, and statistics. | [#288](https://github.com/nuxencs/moviepickarr/pull/288) |
| High | IMDb identities were neither canonical nor unique, allowing duplicate library identities and distorted pool, draw, and statistics behavior. | [#289](https://github.com/nuxencs/moviepickarr/pull/289) |
| High | Invalid, foreign-host, or TMDB-only edit links could clear external IDs and derived data or extract a false IMDb identity. | [#292](https://github.com/nuxencs/moviepickarr/pull/292) |
| Medium | Clearing watched time silently succeeded without changing data, while resubmitting an unchanged minute-level value could lose seconds or shift a repeated DST hour. | [#294](https://github.com/nuxencs/moviepickarr/pull/294) |

### Modal, routing, and deletion ownership

| Impact | Finding | Fix |
|---|---|---|
| Medium | Archived attribution linked to an absent member and silently landed on the viewer's board. | [#263](https://github.com/nuxencs/moviepickarr/pull/263) |
| Medium | A late movie close could pop a newer history entry or close a newer modal. | [#264](https://github.com/nuxencs/moviepickarr/pull/264) |
| Medium | Nested portalled dialogs both remained exposed as modal, and focus restoration could target a removed opener. | [#271](https://github.com/nuxencs/moviepickarr/pull/271) |
| Medium | Forward navigation during an exit timer could reopen a modal that the stale timer then unmounted. | [#275](https://github.com/nuxencs/moviepickarr/pull/275) |
| Medium | Cached stash detail stayed deletable while fresh lifecycle state was unknown, and a pending confirmation could submit again. | [#276](https://github.com/nuxencs/moviepickarr/pull/276) |
| Medium | Custom dialogs lacked accessible names, and a removed opener could leave focus on the document body. | [#293](https://github.com/nuxencs/moviepickarr/pull/293) |

### Members moves, focus, and breakpoints

| Impact | Finding | Fix |
|---|---|---|
| Medium | CSS and JavaScript disagreed at fractional widths around the 761 px push breakpoint, which could focus a hidden control. | [#267](https://github.com/nuxencs/moviepickarr/pull/267) |
| Medium | A pending move belonged to a keyed tile, so an unmount could permit a duplicate request and a stale focus callback. | [#269](https://github.com/nuxencs/moviepickarr/pull/269) |
| Medium | The stash roving tab stop followed an array index rather than the focused movie across live reordering. | [#270](https://github.com/nuxencs/moviepickarr/pull/270) |
| Low | Programmatic move activation claimed focus ownership even when its source cell was not focused. | [#277](https://github.com/nuxencs/moviepickarr/pull/277) |

### Responsive layout and frontend work

| Impact | Finding | Fix |
|---|---|---|
| Medium | Long mobile Stats filters clipped their clear action outside the visible grid cell. | [#265](https://github.com/nuxencs/moviepickarr/pull/265) |
| Medium | Every bubbled rail transition triggered a layout read, including poster and color transitions that cannot change rail height. | [#272](https://github.com/nuxencs/moviepickarr/pull/272) |
| Medium | Members fetched 342 px posters for 96 to 126 px desktop slots. The measured production profile cuts poster resource bytes by 70.7%. | [#278](https://github.com/nuxencs/moviepickarr/pull/278) |
| Medium | At 320 and 340 px, the movie modal's inner rail overflowed and clipped external links and Delete. | [#295](https://github.com/nuxencs/moviepickarr/pull/295) |

## Performance result

The detailed production, render, static, and targeted layout profile is in
[develop-range-perf-review-2026-07-30.md](./develop-range-perf-review-2026-07-30.md).

The live Members profile at #278 remained a Lighthouse 99. Its 17 TMDB poster
resources fell from 396,012 bytes to 116,232 bytes. Estimated image-delivery
waste fell from 367,168 bytes to 16,809 bytes. No suspicious render churn
appeared in the exercised interactions.

Later persistence fixes generally reduce database work. Movie creation removes
one write. Changed-identity edits fall from about seven statements and four
commits to five statements and one commit after the required derived-data
cleanup. Enrichment falls from three commits to one, and draw repository calls
fall from seven to four.

Focused benchmarks on an Apple M4 Pro measured median lock-held work at 0.116
ms for a three-candidate draw, 0.156 ms for a 100-member watch rotation, and
0.251 ms for a move across 100 owned movies. Synthetic extremes reached 6.902
ms for 3,000 draw candidates, 4.927 ms for 10,000 members, and 14.484 ms for a
5,000-movie owner library. The lock itself and synchronous broker fanout were
not material costs. Draw metadata and the move response projection do remain
inside their serialization boundaries; the earlier assessment that reads were
outside every critical section was too broad.

From #278 to #295, the same CI build shows app JavaScript growing by 0.84 kB
gzip and main CSS by 0.13 kB gzip. No chunk exceeds 200 kB gzip and the stack
adds no dependency. The #295 modal reflow is CSS-only and removes horizontal
overflow at 320 and 340 px without viewport reads or render work.

A final cold-cache Lighthouse profile on #295 kept both desktop routes at 99:
Movies had 0.847 s median LCP and Members had 0.784 s. Simulated mobile exposed
a separate pre-range risk. Movies had 3.752 s median LCP, 0.0831 CLS, and 19 ms
TBT; Members had 3.688 s LCP, 0.00002 CLS, and 0 ms TBT. These are lab results,
not field Core Web Vitals.

## Residual findings

- Hero content and controls wait for the remote backdrop to decode. A slow or
  stalled image can keep a known draw blank, and same-draw enrichment can leave
  the painted backdrop stale because its reveal id does not change. Both paths
  predate the reviewed range.
- Member create, restore, archive, and delete events do not invalidate cached
  next-up state. Other clients can retain an archived holder or the empty-roster
  result. The omission predates the range and survived the stacked cache fixes.
- A movie move commits before its fallible response projection. A projection
  error can suppress `movie:moved`, and an idempotent retry cannot republish the
  durable change. The sequence predates the range; #291 preserves it inside the
  pool-state serialization boundary.
- New spring-forward wall times that do not exist in the local timezone still
  normalize to a later time. The input and conversion predate this range; #294
  prevents changes to an existing stored timestamp. Repeated fall-back times
  remain ambiguous without an explicit offset choice.
- The main Movies route has measured pre-range image waste. Desktop loaded 19
  fixed `w342` posters, while mobile used a 135 kB `w1280` hero backdrop. A
  `w185` poster candidate and responsive backdrop sources are the next bounded
  experiments.
- Google Fonts remained render-blocking in the final profile. Lighthouse
  estimated about 300 ms of mobile FCP opportunity, although `display=swap`
  passed and field impact remains unknown.
- The Safari 26 `sizes="auto"` fallback is conservative near column resets but
  remains no worse than the original fixed `w342` behavior. This is an accepted
  compatibility tradeoff, not a logic defect.
- Indexed authentication checks added about 3.2 to 3.6 microseconds on affected
  paths. Migration 010 completed 100,000 rows with 10% identity conflicts in
  213 ms, and a 50,000-row Bolt import completed in 1.92 s. No change is
  warranted from those results.
- Issue [#157](https://github.com/nuxencs/moviepickarr/issues/157) is covered by
  #282, #283, and #285. It remains open, and #285 targets an intermediate stack
  branch, so verify or close the issue after the full stack reaches `develop`.

## Verification

- Frontend: 39 files and 553 tests passed with `bun run test`; TypeScript and
  the production build passed. ESLint reported no errors and one existing Fast
  Refresh warning at `web/src/api/queries.tsx:134`.
- Backend: `go test -race -count=1 ./...` passed. `golangci-lint` reported zero
  issues, and `govulncheck` found no reachable vulnerability.
- Source hygiene: `git diff --check` passed.
- Browser: the authenticated production build was checked at desktop and
  mobile breakpoints. Nested confirmation ownership, interrupted modal history,
  and the 320/340/370/371 px movie-modal boundary were exercised without
  submitting a destructive mutation.
- Performance follow-up: twelve cold-cache Lighthouse runs covered Movies and
  Members on desktop and simulated mobile. Focused serialization, authentication,
  migration, import, and broker benchmarks used disposable SQLite fixtures.
- CI: all 32 fix pull requests had completed, successful checks at handoff. Pull
  request #295 passed all seven of its checks; the documentation pull request
  passed its documentation check.
