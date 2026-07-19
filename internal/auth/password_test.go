package auth

import (
	"strings"
	"testing"

	"github.com/alexedwards/argon2id"
)

func TestHashPassword_ProducesPHCString(t *testing.T) {
	hash, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	// PHC format: $argon2id$v=19$m=19456,t=2,p=1$<salt>$<key>
	if !strings.HasPrefix(hash, "$argon2id$v=19$") {
		t.Errorf("hash is not a PHC argon2id string: %q", hash)
	}
	if !strings.Contains(hash, "m=19456,t=2,p=1") {
		t.Errorf("hash does not encode the configured params: %q", hash)
	}
	if len(strings.Split(hash, "$")) != 6 {
		t.Errorf("hash does not have the 6 PHC segments: %q", hash)
	}
}

func TestHashPassword_SaltedPerHash(t *testing.T) {
	// Same password hashed twice must differ: a per-hash random salt, not a
	// deterministic digest.
	const pw = "same-password"
	a, err := HashPassword(pw)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	b, err := HashPassword(pw)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if a == b {
		t.Error("two hashes of the same password are identical; salt is not random")
	}
}

func TestVerifyPassword_AcceptsCorrectRejectsWrong(t *testing.T) {
	const pw = "s3cret-passphrase"
	hash, err := HashPassword(pw)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}

	match, needsRehash, err := VerifyPassword(pw, hash)
	if err != nil {
		t.Fatalf("VerifyPassword(correct): %v", err)
	}
	if !match {
		t.Error("correct password did not verify")
	}
	if needsRehash {
		t.Error("fresh hash reported needsRehash; params should already match")
	}

	match, _, err = VerifyPassword("wrong-password", hash)
	if err != nil {
		t.Fatalf("VerifyPassword(wrong): unexpected error %v", err)
	}
	if match {
		t.Error("wrong password verified")
	}
}

func TestVerifyPassword_RehashSignalOnParamMismatch(t *testing.T) {
	// A hash created with stronger-than-configured params still verifies, but
	// must flag needsRehash so the login path can upgrade it. The library
	// default (64 MiB, t=3, p=2) differs from our OWASP-minimum configuration.
	const pw = "rehash-me"
	stale, err := argon2id.CreateHash(pw, argon2id.DefaultParams)
	if err != nil {
		t.Fatalf("CreateHash(DefaultParams): %v", err)
	}
	if paramsEqual(argon2id.DefaultParams, configuredParams) {
		t.Fatal("test precondition broken: DefaultParams equals configuredParams")
	}

	match, needsRehash, err := VerifyPassword(pw, stale)
	if err != nil {
		t.Fatalf("VerifyPassword: %v", err)
	}
	if !match {
		t.Error("password with stale params did not verify")
	}
	if !needsRehash {
		t.Error("stale-param hash did not signal needsRehash")
	}
}

func TestVerifyPassword_NoRehashSignalOnWrongPassword(t *testing.T) {
	// needsRehash must never be true when the password is wrong, even if the
	// stored hash has stale params: a failed login must not trigger a rehash.
	stale, err := argon2id.CreateHash("real", argon2id.DefaultParams)
	if err != nil {
		t.Fatalf("CreateHash: %v", err)
	}
	match, needsRehash, err := VerifyPassword("wrong", stale)
	if err != nil {
		t.Fatalf("VerifyPassword: %v", err)
	}
	if match {
		t.Error("wrong password verified")
	}
	if needsRehash {
		t.Error("failed verify reported needsRehash")
	}
}

func TestVerifyPassword_MalformedHash(t *testing.T) {
	for _, hash := range []string{
		"",
		"not-a-phc-string",
		"$argon2id$v=19$m=19456,t=2,p=1$badsalt", // too few segments
		"$bcrypt$v=19$whatever$salt$key",         // wrong variant
	} {
		match, needsRehash, err := VerifyPassword("pw", hash)
		if err == nil {
			t.Errorf("VerifyPassword(%q): expected error, got nil", hash)
		}
		if match {
			t.Errorf("VerifyPassword(%q): match=true on malformed hash", hash)
		}
		if needsRehash {
			t.Errorf("VerifyPassword(%q): needsRehash=true on malformed hash", hash)
		}
	}
}

func TestConfiguredParams_MatchOWASPMinimum(t *testing.T) {
	// Pin the params so an accidental edit can't silently weaken hashing.
	want := argon2id.Params{Memory: 19456, Iterations: 2, Parallelism: 1, SaltLength: 16, KeyLength: 32}
	if !paramsEqual(configuredParams, &want) {
		t.Errorf("configuredParams = %+v, want %+v", *configuredParams, want)
	}
}

func TestDummyVerify_RunsWithoutPanic(t *testing.T) {
	// The dummy hash must be a valid argon2id string so the verify actually runs
	// (and burns the equivalent time). It always returns non-match, but the
	// point is that it doesn't error or panic on any input.
	DummyVerify("anything")
	DummyVerify("")

	if _, err := argon2id.ComparePasswordAndHash("x", dummyHash); err != nil {
		t.Errorf("dummyHash is not a valid argon2id hash: %v", err)
	}
}
