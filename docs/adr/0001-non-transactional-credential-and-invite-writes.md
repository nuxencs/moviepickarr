# ADR 0001: Non-transactional credential and invite writes

Status: accepted (2026-07-19)

ADR 0002 later adopted scoped transactions for cross-table writes whose partial
state breaks a domain invariant. The two recoverable flows named here remain
non-transactional.

## Context

Two write paths in the invite/claim flow (#97) touch more than one repository in
sequence, with no transaction spanning them:

1. `POST /members` creates the member row (`UserRepo`), then issues its first
   invite (`InviteRepo`) in a second step.
2. The password claim writes the credential (`LocalAccountRepo`), then the HTTP
   layer revokes existing sessions and mints a new one (`SessionRepo`), then
   consumes the invite (`InviteRepo`).

A DB error partway through either sequence leaves the writes that already
committed in place. Making these atomic would need a transaction spanning two
different repositories.

## Decision

Leave both non-transactional. Order the effects so the recoverable one lands
last, and rely on the resulting states being benign:

- Create-then-issue: a placeholder with no invite is a legitimate steady state
  (it is exactly a member before you invite them). The admin re-issues to get a
  link; nothing is lost.
- Claim: the invite is consumed last (after the session revoke and mint), so a
  failure before that leaves the link usable. The residual, credential committed
  but session revoke failed, matches what `handleSetLocalLogin` (admin reset) and
  `handleChangePassword` already accept: the new password works on the next
  login, and stale sessions age out on their own windows.

## Consequences

- No cross-repository transaction machinery. This preserves the existing shape:
  single-writer pool, each repository owns its own statements, and the HTTP layer
  owns sessions so the service modules never touch a session.
- The failure states above are recoverable, not corrupting: no partial write
  leaves a member unable to log in or an invite stuck in a broken state.
- ADR 0002's scoped unit-of-work seam does not reopen these two flows. Revisit
  them only if their accepted residual states stop being valid.
