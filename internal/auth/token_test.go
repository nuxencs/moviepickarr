package auth

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"testing"
)

func TestGenerateToken_Shape(t *testing.T) {
	tok, err := GenerateToken()
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}

	// 32 bytes of entropy base64url-encode to 43 unpadded chars.
	if len(tok.Raw) != 43 {
		t.Errorf("Raw length = %d, want 43", len(tok.Raw))
	}

	// Decoding must yield exactly tokenBytes with no padding (RawURLEncoding).
	raw, err := base64.RawURLEncoding.DecodeString(tok.Raw)
	if err != nil {
		t.Fatalf("Raw is not base64url-unpadded: %v", err)
	}
	if len(raw) != tokenBytes {
		t.Errorf("decoded entropy = %d bytes, want %d", len(raw), tokenBytes)
	}
}

func TestGenerateToken_HashMatchesRaw(t *testing.T) {
	tok, err := GenerateToken()
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}

	// Hash is SHA-256(Raw), hex-encoded, and reproducible from the raw token so
	// an inbound cookie can be matched against the stored token_hash.
	if tok.Hash != HashToken(tok.Raw) {
		t.Errorf("Token.Hash = %q, want HashToken(Raw) = %q", tok.Hash, HashToken(tok.Raw))
	}
	want := hex.EncodeToString(sha256Sum(tok.Raw))
	if tok.Hash != want {
		t.Errorf("Hash = %q, want %q", tok.Hash, want)
	}
	if len(tok.Hash) != hex.EncodedLen(sha256.Size) {
		t.Errorf("Hash length = %d, want %d", len(tok.Hash), hex.EncodedLen(sha256.Size))
	}
}

func TestGenerateToken_NeverPersistsRaw(t *testing.T) {
	// The whole point of the hash-only design: the stored hash must not reveal
	// the raw token. They must differ, and the hash must not contain the raw.
	tok, err := GenerateToken()
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}
	if tok.Hash == tok.Raw {
		t.Error("Hash equals Raw; the raw token would be persisted verbatim")
	}
}

func TestGenerateToken_Unique(t *testing.T) {
	// Two calls must never collide; a repeated token would be a CSPRNG failure.
	const n = 1000
	seenRaw := make(map[string]struct{}, n)
	seenHash := make(map[string]struct{}, n)
	for i := range n {
		tok, err := GenerateToken()
		if err != nil {
			t.Fatalf("GenerateToken #%d: %v", i, err)
		}
		if _, dup := seenRaw[tok.Raw]; dup {
			t.Fatalf("duplicate raw token at iteration %d", i)
		}
		if _, dup := seenHash[tok.Hash]; dup {
			t.Fatalf("duplicate hash at iteration %d", i)
		}
		seenRaw[tok.Raw] = struct{}{}
		seenHash[tok.Hash] = struct{}{}
	}
}

func TestHashToken_Deterministic(t *testing.T) {
	const raw = "some-fixed-token-value"
	first := HashToken(raw)
	second := HashToken(raw)
	if first != second {
		t.Error("HashToken is not deterministic for the same input")
	}
	if HashToken(raw) == HashToken(raw+"x") {
		t.Error("HashToken collides for different inputs")
	}
}

func sha256Sum(s string) []byte {
	sum := sha256.Sum256([]byte(s))
	return sum[:]
}
