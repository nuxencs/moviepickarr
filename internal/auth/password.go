package auth

import "github.com/alexedwards/argon2id"

// configuredParams are the argon2id parameters every new hash is created with:
// OWASP's minimum for argon2id (m=19456 KiB / 19 MiB, t=2, p=1, 16-byte salt,
// 32-byte key). Chosen for a small single-box deployment where peak memory, not
// throughput, is the binding constraint; raise Memory if the host has headroom
// (target ~250-500 ms per hash) and the rehash-on-login path upgrades stored
// hashes transparently. Rationale: docs/research/password-hashing.md (#72).
var configuredParams = &argon2id.Params{
	Memory:      19456,
	Iterations:  2,
	Parallelism: 1,
	SaltLength:  16,
	KeyLength:   32,
}

// HashPassword returns a PHC-encoded argon2id hash of password using the
// configured parameters. The full PHC string ($argon2id$v=19$m=...$salt$hash)
// is what callers persist; it carries its own salt and params, so verification
// needs nothing but the stored string. Length validation (min 8, max 128) is a
// caller concern applied before this point; the max in particular guards the
// unbounded-input argon2id DoS and is enforced at the HTTP edge, not here.
func HashPassword(password string) (string, error) {
	return argon2id.CreateHash(password, configuredParams)
}

// VerifyPassword checks password against a stored PHC hash in constant time.
//
// match is true only when the password is correct. needsRehash is true when the
// password verified but the stored hash was encoded with parameters other than
// the configured ones: the caller should recompute HashPassword from the
// just-verified plaintext and store the fresh hash (rehash-on-login), which
// transparently upgrades old hashes as parameters are raised over time.
// needsRehash is never true on a failed verify.
//
// A malformed or unparseable hash returns a non-nil error with match=false;
// callers treat that as a failed login.
func VerifyPassword(password, hash string) (match bool, needsRehash bool, err error) {
	match, params, err := argon2id.CheckHash(password, hash)
	if err != nil {
		return false, false, err
	}
	if !match {
		return false, false, nil
	}
	return true, !paramsEqual(params, configuredParams), nil
}

// DummyVerify runs one argon2id verify against a fixed in-package hash and
// throws the result away. Login paths call it on the branches that have no real
// hash to check (unknown username, member without a local login, locked-out
// account) so those cost the same wall-clock as a genuine wrong-password
// verify. Without it, a fast "no such user" response would leak which usernames
// exist. It does no useful work beyond burning the equivalent CPU/memory.
func DummyVerify(password string) {
	_, _ = argon2id.ComparePasswordAndHash(password, dummyHash)
}

// dummyHash is a real argon2id hash of a throwaway password, computed once at
// package initialization so DummyVerify has a valid PHC string to grind against
// with the configured cost. Precomputing at init (rather than lazily) keeps the
// login hot path free of a one-time hashing spike. A failure here means the
// CSPRNG is unavailable, which is fatal for the whole auth layer, so panic.
var dummyHash = mustDummyHash()

func mustDummyHash() string {
	hash, err := HashPassword("timing-equalization-dummy-password")
	if err != nil {
		panic("auth: precomputing dummy argon2id hash: " + err.Error())
	}
	return hash
}

// paramsEqual reports whether two parameter sets are identical across every
// field that affects the derived hash, so VerifyPassword can tell a
// current-params hash from a stale one that needs rehashing.
func paramsEqual(a, b *argon2id.Params) bool {
	return a.Memory == b.Memory &&
		a.Iterations == b.Iterations &&
		a.Parallelism == b.Parallelism &&
		a.SaltLength == b.SaltLength &&
		a.KeyLength == b.KeyLength
}
