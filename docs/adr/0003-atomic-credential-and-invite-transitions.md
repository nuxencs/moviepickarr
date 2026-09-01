# ADR 0003: Atomic credential and invite transitions

Status: accepted (2026-08-03)

## Context

[ADR 0001](0001-non-transactional-credential-and-invite-writes.md) accepted
separate commits for password claims because the remaining states appeared
recoverable. That ordering leaves a security-sensitive gap: a
credential can change while its invite remains reusable. A later failure or a
competing credential write can therefore leave a live recovery link able to
change the credential again. Direct local-login and identity-link writes can
also leave an older invite current when they commit separately.

Session revocation belongs to the same recovery transition. Committing a new
password or identity without ending the prior sessions does not complete the
recovery guarantee.

The admin API had a separate stale-write problem. Member-scoped actions and
store-local integer invite ids did not identify the generation the admin saw.
A stale tab could act on a replacement, and SQLite can reuse an integer row id
after deletion.

SQLite's single writer serializes these statements, but serialization alone
does not make several commits atomic.

## Decision

Use narrow, use-case-specific writer transactions for every transition where a
credential write must retire or consume an invite:

- `RedeemPasswordInvite` validates the token hash authoritatively inside the
  transaction, creates or resets the local credential, replaces every existing
  session, and marks that exact invite used.
- `RedeemOIDCInvite` validates an onboarding token hash, rejects a generation
  for an existing local login, links the verified identity, replaces every
  existing session, and marks that exact invite used.
- `SetLocalCredential` creates or resets a local credential and retires any
  current invite. An admin reset revokes the member's existing sessions in the
  same transaction. A signed-in member adding their first local credential
  keeps the session that authorized the operation.
- `ChangeVerifiedPassword` compares the hash verified outside SQLite, changes
  the password, revokes the old sessions, and retires any current reset invite.
  The hash comparison prevents a concurrent reset from being overwritten.
- `CompleteLocalLogin` records a verified login and creates its session under
  the expected password hash. If recovery commits first, the old-password login
  cannot restore a maintenance rehash or mint a session. If login commits first,
  recovery deletes that session.
- `CompleteOIDCLogin` refreshes a verified linked identity and creates its
  session in one transaction. An unlink that commits first leaves no credential
  for the login transaction to match.
- `DeleteLocalCredential` removes the password and retires any current reset
  invite, while applying the self-last-credential guard in that transaction.
- `LinkOIDCAndRetireInvite` links a verified identity and retires any current
  invite in one transaction.
- `DeleteOIDCIdentity` removes a linked identity and retires any current reset
  invite under the same self-last-credential guard. Removing an identity must
  not broaden an existing reset link's claim options.
- The break-glass admin seed retires an adopted member's current invite in the
  transaction that creates the local credential.
- `CreateMemberWithInvite` inserts the placeholder, assigns an eligible role to
  `next_up` only when no active holder resolves, inserts its first invite, and
  reads the response member before commit. Concurrent creates queue on SQLite's
  writer, so the first eligible inserted member keeps an initially unresolved
  turn.
- `RestoreMemberWithInvite` strips residual credentials, identities, sessions,
  and invites, clears `archived_at`, inserts a fresh invite, and reads the
  response member before commit.

These operations form the `AuthTransitionStore` and
`MemberInviteTransitionStore` boundaries, implemented by the same scoped
SQLite store. Every authoritative invite, credential, and membership read used
by a transition runs on its transaction.
Password hashing and the OIDC provider round trip happen before it, outside
SQLite's writer. The HTTP layer prepares opaque session tokens before the
transition and sets their cookies after commit. Local, OIDC, and recovery-claim
session rows commit with the credential check that authorized them. A session
insert failure rolls the transition back, leaving the prior credential and
invite usable for a retry.

Give every invite generation an immutable random `public_id`. The raw claim
token remains a separate secret and only its SHA-256 hash is stored. A partial
unique index on `user_id` permits at most one unused, unrevoked generation for a
member. Expiry does not remove a generation from that invariant, so an expired
invite stays current until it is replaced, revoked, dismissed, used, or retired
by a credential transition.

Admin mutations address the exact public generation:

- `POST /members/:memberID/invite` creates the first current generation. The
  default purpose is onboarding and requires a credential-less member. A body
  of `{"purpose":"password_reset"}` explicitly requests recovery for a member
  with a local login. Reset generations expose only the password claim path;
  OIDC initiation and the transaction both reject them.
- `POST /invites/:inviteID/replacement` retires the addressed generation and
  creates its replacement in one transaction.
- `DELETE /invites/:inviteID` revokes the addressed generation only while it is
  open.
- `POST /invites/:inviteID/dismiss` retires the addressed generation only after
  it expires.

A stale or wrong-state public handle returns a conflict and cannot affect a
newer generation. Claim redemption still resolves by token hash, then consumes
the same internal row id and token hash inside that transaction.

`GET /invites` returns `{serverNow, items}`. Items include current onboarding
and password-reset generations, with open or expired derived from the same
second-precision server clock. This keeps reset links manageable after the
member already has a credential and gives the client a clock anchor for expiry.

Member create and restore prepare the random public handle and raw token before
opening the writer transaction. Only the hash enters SQLite. The raw token is
returned after commit in the direct HTTP response and is never broadcast. The
response member projection is read inside the transaction, so a later database
read cannot strand the only copy of a committed claim URL behind a 500 response.

## Consequences

- A failed credential, invite, or recovery-session write rolls back the whole
  transition. No failure can leave a changed credential beside a reusable
  invite.
- Password and OIDC claims replace prior sessions in the credential and invite
  transaction, including a credential-less placeholder with residual sessions.
- A failed onboarding invite insert rolls back the member and initial
  `next_up` assignment. A failed restored-member invite insert rolls back the
  authentication cleanup and reactivation.
- Concurrent creates cannot overwrite the first new member's initial
  `next_up` assignment.
- An in-flight old-password login cannot outlive or overwrite a committed
  recovery transition.
- Direct credential setup, change, removal, and identity linking cannot leave
  an older onboarding or reset link live.
- Exact public handles prevent stale admin views and integer-id reuse from
  targeting a replacement generation.
- Expired and password-reset generations remain in the admin overview until an
  explicit lifecycle action retires them.
- Auth transition tests must inject failures inside SQLite transactions and
  prove rollback rather than stop at returned errors.
