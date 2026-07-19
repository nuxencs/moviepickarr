package auth

import (
	"testing"
	"time"
)

func txClock(t time.Time) func() time.Time { return func() time.Time { return t } }

func TestOIDCTxCodec_SealOpenRoundTrip(t *testing.T) {
	codec, err := NewOIDCTxCodec("a secret")
	if err != nil {
		t.Fatalf("new codec: %v", err)
	}
	in := OIDCTx{State: "st", Nonce: "no", PKCEVerifier: "pk", Intent: IntentLink, MemberID: 7, InviteTokenHash: "hash"}

	sealed, err := codec.Seal(in)
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	out, err := codec.Open(sealed)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if out.State != in.State || out.Nonce != in.Nonce || out.PKCEVerifier != in.PKCEVerifier ||
		out.Intent != in.Intent || out.MemberID != in.MemberID || out.InviteTokenHash != in.InviteTokenHash {
		t.Fatalf("round trip mismatch: %+v vs %+v", out, in)
	}
}

func TestOIDCTxCodec_TamperRejected(t *testing.T) {
	codec, _ := NewOIDCTxCodec("a secret")
	sealed, _ := codec.Seal(OIDCTx{State: "st", Intent: IntentLogin})

	b := []byte(sealed)
	b[len(b)-1] ^= 0x01 // flip a bit in the tag
	if _, err := codec.Open(string(b)); err != ErrTxInvalid {
		t.Fatalf("tampered open = %v, want ErrTxInvalid", err)
	}
	if _, err := codec.Open(""); err != ErrTxInvalid {
		t.Fatalf("empty open = %v, want ErrTxInvalid", err)
	}
	if _, err := codec.Open("not base64!!"); err != ErrTxInvalid {
		t.Fatalf("garbage open = %v, want ErrTxInvalid", err)
	}
}

func TestOIDCTxCodec_WrongKeyRejected(t *testing.T) {
	a, _ := NewOIDCTxCodec("secret A")
	b, _ := NewOIDCTxCodec("secret B")
	sealed, _ := a.Seal(OIDCTx{State: "st", Intent: IntentLogin})
	if _, err := b.Open(sealed); err != ErrTxInvalid {
		t.Fatalf("cross-key open = %v, want ErrTxInvalid", err)
	}
}

func TestOIDCTxCodec_Expiry(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	clk := now
	codec, _ := NewOIDCTxCodec("a secret", WithTxClock(func() time.Time { return clk }))
	sealed, _ := codec.Seal(OIDCTx{State: "st", Intent: IntentLogin})

	// Just inside the window: still valid.
	clk = now.Add(OIDCTxTTL - time.Second)
	if _, err := codec.Open(sealed); err != nil {
		t.Fatalf("open within TTL = %v, want ok", err)
	}
	// Past the window: rejected.
	clk = now.Add(OIDCTxTTL + time.Second)
	if _, err := codec.Open(sealed); err != ErrTxInvalid {
		t.Fatalf("open past TTL = %v, want ErrTxInvalid", err)
	}
}

func TestPKCEChallengeS256(t *testing.T) {
	// RFC 7636 Appendix B worked example.
	const verifier = "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	const want = "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"
	if got := pkceChallengeS256(verifier); got != want {
		t.Fatalf("challenge = %q, want %q", got, want)
	}
}
