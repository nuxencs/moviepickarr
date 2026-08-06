# Admin Integrations Review Handoff: 2026-08-05

Status: complete

Scope: the complete uncommitted worktree against `develop` at
`8de06730141b596709b9ea6739bc06526d79518f`. The review covered 27 modified
tracked files and 65 untracked files.

Spec baseline: `develop:docs/admin-integrations.md` and accepted ADRs 0004,
0005, and 0006.

## Implementation order

Each item gets a focused regression test where practical. Finish its focused
gate before starting the next item.

- [x] P1: derive the visible integration state when an environment key makes
  TMDB active. Fresh and migrated env-key deployments must not remain
  `Disabled` or lose manual actions.
- [x] P1: ignore stale authentication-rejection callbacks. A late request from
  an old runtime generation must not pause a recovered scheduler.
- [x] P1: do not reapply an old persisted credential rejection to a rotated
  environment key after restart.
- [x] P2: retry scheduled-run discovery after a failed fetch or a race with
  ledger creation.
- [x] P2: implement the persistent `/admin/integrations` index with integration
  state and latest activity.
- [x] P2: update `Last successful refresh` only for a completed run without
  errors.
- [x] P2: record the initiating admin for configuration-triggered refreshes.
- [x] P2: preserve the actual trigger for single-movie enrichment started by an
  identity edit.
- [x] P2: keep connection-test timestamps separate from `Last library scan`.
- [x] P2: record shutdown-cancelled single-movie work as `Interrupted`.
- [x] P2: keep the active environment value visible while staging removal of a
  dormant fallback.
- [x] P2: make worker startup logs describe the effective runtime
  configuration, or omit inactive legacy values.
- [x] P2: use a component logger, static message, and structured fields for
  environment configuration issues.
- [x] P2: move shared source, field, secret, precedence, and default mechanics
  from the TMDB module into `internal/integration`.
- [x] P2: use the documented MG input and button vocabulary on the new Admin
  surfaces.
- [x] P2: rewrite new Admin render tests around roles, text, and observable
  behavior instead of classes and internal structure.
- [x] P3: replace `.sr-only` with the documented `.vis-hidden` utility.
- [x] P3: use `plural()` for count-based integration copy.

## Baseline verification

- `make lint`: passed, with one pre-existing Fast Refresh warning in
  `web/src/api/queries.tsx`.
- `make test`: passed, 60 frontend files and 724 tests plus Go race tests.
- `make build`: passed.
- Targeted backend race tests: passed.
- `git diff --check develop`: passed.
- Browser smoke check: desktop and 375 px mobile TMDB settings and run history,
  including the unsaved-navigation confirmation. No document overflow or browser
  console warnings and errors.

## Completion gate

- Focused regression test for every behavior change where practical.
- `make lint`
- `make test`
- `make build`
- `git diff --check develop`
- Desktop and mobile browser check for changed Admin flows.
