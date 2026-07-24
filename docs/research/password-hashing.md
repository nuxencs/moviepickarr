# Research: password hashing for local auth

Issue: [#72](https://github.com/nuxencs/moviepickarr/issues/72). Researched 2026-07-18 against primary sources (OWASP, RFC 9106, pkg.go.dev, library repos). Context: the reverted auth attempt (PR #26, reverted in #32) hand-rolled argon2id on `golang.org/x/crypto/argon2` with `m=64 MiB, t=3, p=2` and a custom (slightly non-standard) hash encoding.

## Recommendation

Use **argon2id** via **`github.com/alexedwards/argon2id`**, with parameters:

```go
&argon2id.Params{
    Memory:      19456, // KiB = 19 MiB (OWASP minimum)
    Iterations:  2,
    Parallelism: 1,
    SaltLength:  16,
    KeyLength:   32,
}
```

Rationale in one line: argon2id is OWASP's first choice and RFC-specified, the library adds the PHC encode/verify layer that raw `x/crypto/argon2` lacks, and its only dependency is `golang.org/x/crypto`, which the module would need anyway.

If the deployment box has RAM to spare, the library default (`m=65536` KiB / 64 MiB, `t=3`, `p=2`) is a fine stronger setting; benchmark on the target hardware and aim for ~250-500 ms per hash. Store the full PHC string and rehash on login when stored params differ from current params (`argon2id.CheckHash` returns the parsed params for exactly this).

## Algorithm comparison

### OWASP Password Storage Cheat Sheet

Source: <https://cheatsheetseries.owasp.org/cheatsheets/Password_Storage_Cheat_Sheet.html> (living document, accessed 2026-07-18).

Preference order:

1. **argon2id** (primary recommendation)
2. **scrypt** where argon2id is unavailable
3. **bcrypt** for legacy systems
4. **PBKDF2** where FIPS-140 compliance is required

Exact parameter minimums:

- argon2id: `m=19456` (19 MiB), `t=2`, `p=1`. Listed equivalents trading memory for iterations: `m=47104,t=1,p=1`; `m=12288,t=3,p=1`; `m=9216,t=4,p=1`; `m=7168,t=5,p=1`.
- scrypt: `N=2^17` (128 MiB), `r=8`, `p=1`.
- bcrypt: work factor 10 or more; OWASP notes "bcrypt has a maximum length input length of 72 bytes" and prescribes an HMAC-then-base64 pre-hash if you must work around it.

### RFC 9106 (Argon2, IRTF, September 2021)

Source: <https://www.rfc-editor.org/rfc/rfc9106.html>, section 4.

- First recommended option: `t=1, p=4, m=2^21` (2 GiB), 128-bit salt, 256-bit tag ("if much memory is available").
- Second recommended option: `t=3, p=4, m=2^16` (64 MiB), same salt/tag ("if much less memory is available").
- Variant choice: "If you do not know the difference between the types or you consider side-channel attacks to be a viable threat, choose Argon2id." Argon2id hybridizes Argon2i (side-channel resistant first pass) and Argon2d (stronger brute-force cost).

Note the `p` tension: OWASP's floor uses `p=1` (tuned for web servers handling requests on one core each), RFC 9106 uses `p=4` (assumes spare cores). For a single small box, `p=1` is the safer default; on a Raspberry Pi class machine extra lanes may not help anyway.

### Security posture, 2026

- bcrypt is still acceptable but second tier: compute-hard, not memory-hard, so GPU/ASIC attacks scale against it much better than against argon2id. Plus the 72-byte input cap. Fine for existing deployments; not the pick for greenfield code.
- argon2id's advantage is memory-hardness: each guess must touch tens of MiB, and memory (bandwidth and capacity) stays expensive on GPUs/ASICs where raw compute is cheap.
- scrypt is also memory-hard and fine, but OWASP positions it strictly as the fallback when argon2id is unavailable. In Go both are equally available, so there is no reason to prefer it.
- NIST SP 800-63B-4 (finalized 2025) directs memory-hard key derivation for password storage. NIST's FAQ (<https://pages.nist.gov/800-63-FAQ/>) prefers functions built on approved primitives and mentions BALLOON, noting Argon2 does "not use an underlying one-way function that has been thoroughly analyzed"; industry practice still treats argon2id as the default. (Paraphrased from the FAQ, not the final PDF clause; we are not FIPS-bound, so this does not change the recommendation.)

## Go libraries

### golang.org/x/crypto (Go team, latest v0.54.0, published 2026-07-08)

Actively maintained module with roughly monthly tags; mirror at <https://github.com/golang/crypto>.

- `x/crypto/argon2` (<https://pkg.go.dev/golang.org/x/crypto/argon2>): exports only raw derivation, `IDKey(password, salt, time, memory, threads, keyLen)`. No salt generation, no PHC `$argon2id$v=19$...` encoding, no verify helper. Using it directly means hand-rolling all of that, which is what PR #26 did (and its custom `argon2id$...` format was not even standard PHC).
- `x/crypto/bcrypt` (<https://pkg.go.dev/golang.org/x/crypto/bcrypt>): `MinCost=4`, `DefaultCost=10`, `MaxCost=31`. Since the "reject passwords longer than 72 bytes" commit ([bc7d1d1](https://github.com/golang/crypto/commit/bc7d1d1eb54b3530da4f5ec31625c95d7df40231), 2022-12-21, shipped in v0.5.0, early 2023), `GenerateFromPassword` returns `ErrPasswordTooLong` for >72-byte input instead of silently truncating; `CompareHashAndPassword` still compares only the first 72 bytes. So Go's bcrypt no longer truncates silently, but the cap becomes a user-facing error you have to handle (long passphrases exist).
- `x/crypto/scrypt`: raw `Key(...)` only, same encoding gap as argon2.

### github.com/alexedwards/argon2id (recommended)

Source: <https://github.com/alexedwards/argon2id> (accessed 2026-07-18).

- API: `CreateHash(password, params)` returns a standard PHC string (`$argon2id$v=19$m=...,t=...,p=...$salt$hash`); `ComparePasswordAndHash` does constant-time verify with params parsed from the string; `CheckHash` also returns the parsed params for rehash-on-login upgrades.
- `DefaultParams`: `Memory=65536` (64 MiB), `Iterations=3`, `Parallelism=2`, `SaltLength=16`, `KeyLength=32`.
- Health: 673 stars, 4 open issues, latest release v1.0.0 (2023-10-21) but commits through 2025-10-28 (merged PRs #24, #27; verified via the GitHub API). Small, stable, effectively "done" software of a few hundred lines.
- Dependency footprint: wraps `x/crypto/argon2`; its only external dependency is `golang.org/x/crypto`.

### Alternatives

- `github.com/matthewhartstonge/argon2`: also PHC-encoding argon2id wrapper, v1.5.6 (July 2026), ~149 stars, 0 open issues, depends only on `x/crypto`. Freshest tags; fine substitute if alexedwards ever goes stale.
- `github.com/go-crypt/crypt`: multi-algorithm hash layer (argon2, bcrypt, scrypt, PBKDF2, crypt formats), ~26 stars. Overkill for one algorithm; only interesting if we ever import hashes from other systems.

## Dependency footprint

The ticket assumed `x/crypto` was already a transitive dependency; it is not. Current `go.mod` (post-revert) has no `golang.org/x/crypto` at all, direct or indirect (`grep golang.org/x/crypto go.sum` returns nothing). Whichever algorithm we pick adds it back. Choosing alexedwards/argon2id adds exactly two modules: the wrapper itself and `golang.org/x/crypto` (Go team maintained). Choosing bcrypt would add only `x/crypto`, but at the cost of the weaker algorithm and the 72-byte cap; not worth saving one tiny, single-purpose module.

## Tuning notes for the deployment target

Low concurrency (a handful of friends, logins rare, sessions long-lived) means hash cost per login is nearly free operationally; the binding constraint is peak memory on a small box.

- OWASP minimum (19 MiB, t=2, p=1): safe on a 1 GB Pi or small VPS even with a few simultaneous logins. This is the floor, and the recommended starting point.
- Library default (64 MiB, t=3, p=2): stronger, still fine at this concurrency on boxes with 1 GB+; drop `p` to 1 on single-core-ish hardware.
- RFC 9106 first option (2 GiB): never on this class of hardware, it will OOM.
- Target ~250-500 ms per hash on the real box; if memory-constrained, raise `t` instead of `m` to hit the time target.
- Optionally serialize hashing with a small semaphore if worried about several logins landing at once on a 512 MB box.

## Unverified details (flagged)

- Exact x/crypto v0.5.0 tag date (pkg.go.dev version page truncated); the bcrypt reject commit date 2022-12-21 and the v0.4.0..v0.5.0 range are confirmed.
- SP 800-63B-4 memory-hard wording is paraphrased from NIST's FAQ, not quoted from the final PDF.

## Sources

- OWASP Password Storage Cheat Sheet: <https://cheatsheetseries.owasp.org/cheatsheets/Password_Storage_Cheat_Sheet.html>
- RFC 9106: <https://www.rfc-editor.org/rfc/rfc9106.html>
- pkg.go.dev: <https://pkg.go.dev/golang.org/x/crypto/argon2>, <https://pkg.go.dev/golang.org/x/crypto/bcrypt>
- bcrypt >72-byte reject: <https://github.com/golang/crypto/commit/bc7d1d1eb54b3530da4f5ec31625c95d7df40231>, <https://github.com/golang/go/issues/36546>
- <https://github.com/alexedwards/argon2id>, <https://github.com/matthewhartstonge/argon2>, <https://github.com/go-crypt/crypt>
- NIST SP 800-63 FAQ: <https://pages.nist.gov/800-63-FAQ/>
