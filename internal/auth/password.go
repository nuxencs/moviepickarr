package auth

import "github.com/alexedwards/argon2id"

// configuredParams is OWASP's argon2id minimum, sized for a small single-box
// host. Raising it upgrades stored hashes on login. See
// docs/research/password-hashing.md (#72).
var configuredParams = &argon2id.Params{
	Memory:      19456,
	Iterations:  2,
	Parallelism: 1,
	SaltLength:  16,
	KeyLength:   32,
}

// HashPassword returns a PHC-encoded argon2id hash, which carries its own salt
// and params. Callers enforce the length bound first.
func HashPassword(password string) (string, error) {
	return argon2id.CreateHash(password, configuredParams)
}

// VerifyPassword checks password against a stored PHC hash in constant time.
// needsRehash is true only on a match whose hash used other params; the caller
// then stores a fresh hash. A malformed hash returns an error.
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

// DummyVerify burns one argon2id verify so a login path with no real hash costs
// the same time as a wrong password, and cannot leak which usernames exist.
func DummyVerify(password string) {
	_, _ = argon2id.ComparePasswordAndHash(password, dummyHash)
}

// dummyHash is computed at init to keep a one-time hashing spike off the login
// path.
var dummyHash = mustDummyHash()

func mustDummyHash() string {
	hash, err := HashPassword("timing-equalization-dummy-password")
	if err != nil {
		panic("auth: precomputing dummy argon2id hash: " + err.Error())
	}
	return hash
}

func paramsEqual(a, b *argon2id.Params) bool {
	return a.Memory == b.Memory &&
		a.Iterations == b.Iterations &&
		a.Parallelism == b.Parallelism &&
		a.SaltLength == b.SaltLength &&
		a.KeyLength == b.KeyLength
}
