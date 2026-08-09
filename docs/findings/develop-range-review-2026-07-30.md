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

No blocking finding remains in the merged stack through pull request #295.
That review produced 32 stacked pull requests and 35 commits. The complete
stack reached `develop` on 2026-08-01.

The highest-impact findings were unrevealed winner leaks, commands that could
publish lifecycle events out of order, writes that could partially commit, and
stale enrichment that could overwrite a newer movie identity. Each fix is a
separate pull request except where one state machine owned both symptoms.
Regression tests were written first where the behavior fit automated testing.

A follow-up audit closed the original measurement gaps and found four defects
that predate the reviewed range. Seven TDD pull requests, #298, #299, and #301
through #305, now address those defects and two measured image/layout gaps.
That follow-up stack remains open and is based on `develop`.

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
| High | A stale enrichment response could overwrite a newer identity, while separate metadata and credit commits could leave mixed-movie data. | [#287](https://github.com/nuxencs/moviepickarr/pull/287) |
| Medium | Changing movie identity could indefinitely expose the previous movie's poster, cast, runtime, and statistics. | [#288](https://github.com/nuxencs/moviepickarr/pull/288) |
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
not material costs. At #295, draw metadata and the move response projection
remained inside their serialization boundaries; the earlier assessment that
reads were outside every critical section was too broad. #299 removes the move
projection from that boundary and was not rebenchmarked.

From #278 to #295, the same CI build shows app JavaScript growing by 0.84 kB
gzip and main CSS by 0.13 kB gzip. No chunk exceeds 200 kB gzip and the stack
adds no dependency. The #295 modal reflow is CSS-only and removes horizontal
overflow at 320 and 340 px without viewport reads or render work.

A final cold-cache Lighthouse profile on #295 kept both desktop routes at 99:
Movies had 0.847 s median LCP and Members had 0.784 s. Simulated mobile exposed
a separate pre-range risk. Movies had 3.752 s median LCP, 0.0831 CLS, and 19 ms
TBT; Members had 3.688 s LCP, 0.00002 CLS, and 0 ms TBT. These are lab results,
not field Core Web Vitals.

The exact-parent #304/#305 comparison isolates the mobile Hero reservation.
Across three cold Movies runs, median CLS fell from 0.43742 to 0, score rose
from 68 to 87, median LCP stayed at 3.760 s, and TBT moved from 3 to 1 ms.
Desktop stayed at 100 with median LCP moving from 0.822 to 0.818 s. The #305
entry chunk is 0.49 kB gzip larger than #295; CSS has no rounded gzip increase.

The follow-up also measured three alternatives that were not retained. A
smaller responsive backdrop cut image bytes but did not improve LCP.
Self-hosted fonts improved LCP while delaying mobile FCP by 453 to 602 ms. A
Movies route split cut unused JavaScript on direct Members visits but delayed
Movies mobile FCP by 221 ms and LCP by 63 ms. The performance review records
the full comparisons and their limits.

## Residual findings

The actionable pre-range residuals now have focused fixes. Measurements that
did not support a change remain explicit.

| Residual | Current result |
|---|---|
| Known Hero content waited for backdrop decode; same-draw updates could leave stale art. | [#302](https://github.com/nuxencs/moviepickarr/pull/302) renders content independently, repaints same-draw artwork changes, rejects stale decodes, and retains fallback art. Remote backdrop delivery still controls image LCP. |
| Roster events left cached next-up state stale. | [#298](https://github.com/nuxencs/moviepickarr/pull/298) adds next-up invalidation to member creation, restoration, archive, and deletion events. Event-burst network impact was not profiled. |
| A committed move could fail during unused response projection and never publish. | [#299](https://github.com/nuxencs/moviepickarr/pull/299) removes the projection, returns `204`, and publishes the changed identifiers directly. The old move benchmark was not rerun. |
| Spring-forward wall times normalized to a different minute. | [#301](https://github.com/nuxencs/moviepickarr/pull/301) rejects the normalized value. Repeated fall-back times remain ambiguous because `datetime-local` carries no offset. |
| Members lacked a middle poster candidate and Movies cards lacked size contracts. | [#303](https://github.com/nuxencs/moviepickarr/pull/303) adds `w185`; [#304](https://github.com/nuxencs/moviepickarr/pull/304) adds pool, watched-card, and row size hints. Final mobile Members runs selected `w185` for all three loaded posters with zero resize waste. |
| Pending mobile Hero content had no reserved height. | [#305](https://github.com/nuxencs/moviepickarr/pull/305) removes the measured shift in its exact-parent comparison. The reservation is a minimum, not a cap for long translated content. |
| Google Fonts remained render-blocking. | Self-hosting was measured and rejected because every route painted later. Google delivery remains pending field cache and FCP evidence. |
| Members loads eager Movies route code. | The measured split helped direct Members visits but regressed the default Movies route. It was removed; route traffic data is needed before revisiting. |
| Other poster surfaces and Safari fallback selection remain unmeasured. | Hero, modal, reel, search, and Stats retain their existing contracts. The conservative Safari 26 fallback remains an accepted compatibility tradeoff. |
| Authentication, migration, and import costs were bounded but not actionable. | Indexed guards added 3.2 to 3.6 microseconds. Migration 010 took 213 ms for 100,000 rows with 10% conflicts; a 50,000-row Bolt import took 1.92 s. No change is warranted. |

Issue [#157](https://github.com/nuxencs/moviepickarr/issues/157) is closed and
fully present on `develop`. #282 makes watch and rotation atomic, #283 makes
identified movie creation atomic, and #285 makes the break-glass seed atomic.
#299 fixes a separate move-publication problem. Invite and claim flows remain
intentionally outside the transaction seam under ADR 0001.

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
- Residual performance pass: matched three-run Lighthouse comparisons covered
  #304/#305 Hero geometry, self-hosted fonts, and the route-split prototype.
  Responsive Hero priority and 320/340 px geometry received targeted runs.
- Current frontend: 41 files and 563 tests pass on #305. Its production build
  completes in 3.18 seconds.
- CI: the 32-pull-request original stack is merged. Every open follow-up pull
  request at #298, #299, and #301 through #305 has seven completed, successful
  checks.
