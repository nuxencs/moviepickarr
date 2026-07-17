# Architecture review — July 2026

Deepening pass over the whole codebase (Go backend + web), driven by the
deep-module vocabulary (module, interface, seam, adapter, depth, locality).
Seven candidates were identified from two exploration sweeps and each was
walked through a decision loop. Everything below is a locked decision, not a
proposal; nothing is built yet.

Evidence highlights that motivated the pass:

- The fact "a Draw happened" exists in seven client-side encodings; the
  reveal-once invariant is guarded in three modules (`DrawReel.confirmedRef`,
  `Hero.revealedDrawRef`, `setActiveSpin` dedup).
- `web/` has zero test files, largely because the draw behaviour is
  orchestration spread across Hero's five chained effects.
- 10 of 12 Go interfaces have one prod implementation; the 4 `Service`
  interfaces have no fakes and no polymorphic consumer.
- The movies 9-column SELECT is copy-pasted 8x; five DTO constructors make
  lean-vs-full a caller obligation.
- The stats cache key, matcher, and echo must fold genre identically across
  three functions, enforced only by comments.
- `defaultAutoRevealDelay` (Go) mirrors `--dur-spin` + `--dur-confirm` (CSS)
  via a "keep in sync" comment.

## 1. Client Draw machine (Strong)

One pure state machine owns the full draw lifecycle: idle, spinning, settled,
revealed, committed. It absorbs draw identity + dedup, the resume decision,
DrawReel's settle/confirm phases, and reveal-once.

- `drawMachine.ts`: pure reducer `(state, event, env) -> [state, commands]`.
  Env snapshot (`spinDurationMs`, `reducedMotion`, `clientId`) resolved by the
  store at send time, so skip/resume/mine decisions are all testable.
- `drawStore.ts`: module singleton (like `handledDraws` today), subscribed via
  `useSyncExternalStore`. Commands as data, executed by injected executors
  (reveal POST, invalidations, backdrop decode, fallback timer). Exactly-once
  rules become reducer tests.
- Deleted: `DrawKeys.active/revealed` cache-as-event-bus, `handledDraws`,
  Hero's ref mirrors, DrawReel's `confirmedRef` and local fallback countdown.
  Easing geometry stays in `drawSpin.ts` for DrawReel rendering.
- Test harness: vitest (chosen over `bun test` for the component-test path).
- Landing: one branch, two commits. Commit 1 additive (machine + store +
  vitest + suite encoding today's behaviour), commit 2 rewires
  Hero/DrawReel/useSSE. Gate: vitest plus /verify-frontend (full draw cycle,
  F5 mid-spin resume, two-tab reveal lockstep).

New glossary term recorded in CONTEXT.md: Settle.

## 2. Server Draw lifecycle (Strong)

`movie.Service` absorbs the auto-reveal timer and reveal notification.

- Construction: `movie.DrawConfig{AutoRevealDelay, Timer, OnRevealed}`;
  the server wires `OnRevealed` to the SSE broadcast. Handler loses
  `scheduleAutoReveal`/`cancelAutoReveal`/`autoReveal`/`broadcastRevealed`
  and the three timer fields; all four funnel paths become internal.
- The server owns reveal timing: `revealAt` (absolute server time) rides the
  drawn payload and the current-movie resume payload. The client countdown is
  `revealAt - serverNow` (skew-free); `--dur-confirm` stops being
  load-bearing; the CSS-mirror comment dies. Client fallback timer =
  `revealAt` + grace. A `--dur-spin` retune now degrades gracefully instead
  of desyncing.
- `ActiveDraw` stays process-local, documented on the interface. A restart
  mid-draw shows the result without ceremony; accepted (rare double-fault,
  state stays correct).
- Tests: dataflow tests drive the lifecycle with a fake timer through the
  Service interface.

## 3. SSE invalidation table + connection reducer (Worth exploring)

- `sseInvalidations.ts`: declarative event -> query-keys table; the switch
  walks it and `resync()` derives from the table's union. A new event type is
  one row. Draw events become `drawStore.send()` dispatches beside it.
- `sseConnection.ts`: pure reducer owning seq gap, epoch/restart, and
  reconnect decisions; `useSSE` shrinks to an EventSource adapter. Both get
  vitest suites.
- Sequenced after candidate 1 so the drawn/revealed cases are rewritten once.

## 4. Interface collapse + Next up module (Worth exploring)

- Delete the four `Service` interface types; handlers hold concrete structs
  (methods unchanged). All six domain repo interfaces stay: three are real
  seams (test fakes cross them), the rest keep the repository seam uniform.
- `advanceNextUp` + `initNextUp` move behind a deepened Next up module:
  `Advance(ctx) -> (nextUp, changed)`; the handler keeps only the broadcast.
  The unused `userRepo` dep in nextup is deleted. Rotation gets tests.
- `movie.Service`'s ~14 pass-through forwards stay (removal is churn without
  gain); revisit only if a handler needs to bypass.

## 5. One movie projection, typed lean/full (Worth exploring)

- Repository: one shared column-list constant + one scan replaces the 8
  copy-pasted SELECT lists.
- DTO seam: split `movieResponse` into `leanMovieTile` and `fullMovie`, one
  builder each; the handler's return type states the payload class, so the
  compiler guards the lean Watched payload (196->16 KB) instead of review.
  Wire JSON stays byte-identical; Response.ts untouched.

## 6. Window + filters as one module, both sides (Worth exploring)

- Server: parse once into a `StatsFilter` value exposing `CacheKey()`,
  `Matches(movie)`, `Echo()` derived from the same folded fields. The
  three-way agreement-by-comment (cache poisoning hazard) is deleted by
  construction.
- Web: one `StatsFilters` object threaded end-to-end (statsSearch produces
  it; query options, keys, and APIClient take the object; canonical
  serialization in one place). Chips stay bespoke; a descriptor registry was
  considered and rejected (year/decade exclusivity and per-chip UX strain a
  generic interface). New dimension: ~4 obvious touches instead of ~9 files.

## 7. useDismissible for floating surfaces (Speculative, accepted)

One hook owning phase (open/closing/closed), the exit timer, the re-entry
guard, and focus restore. Modal, Menu, FilterChipMenu, the Stats range panel,
and the DateRange popover become adapters. Five implementations already prove
the seam. Migrated all at once, verified surface-by-surface; closing act of
the roadmap.

## Build order

Dependency chain: 4 -> 2 -> 1 -> 3 (candidate 2 rebuilds movie.Service
construction, so the interface collapse goes first; the client machine
consumes `revealAt` from 2; the SSE rework follows the machine). Candidates
5, 6, 7 are independent and can interleave.

1. Interface collapse + Next up module (4)
2. Server Draw lifecycle + revealAt (2)
3. Client Draw machine + vitest bootstrap (1)
4. SSE table + connection reducer (3)
5. Movie projection (5)
6. Stats filter module (6)
7. useDismissible (7)
