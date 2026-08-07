package integration

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

type fixedKeySource struct {
	key []byte
	err error
}

func (s fixedKeySource) Key() ([]byte, error) {
	return s.key, s.err
}

func TestSecretStoreRoundTrip(t *testing.T) {
	store := NewSecretStore(fixedKeySource{key: bytes.Repeat([]byte{0x2A}, 32)})

	sealed, err := store.Encrypt("tmdb-secret-value")
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	plaintext, err := store.Decrypt(sealed)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if plaintext != "tmdb-secret-value" {
		t.Fatalf("plaintext = %q, want original secret", plaintext)
	}
}

func TestSecretStoreRejectsNon256BitAESKeys(t *testing.T) {
	t.Parallel()
	for _, size := range []int{16, 24} {
		t.Run(fmt.Sprintf("%d bytes", size), func(t *testing.T) {
			store := NewSecretStore(fixedKeySource{key: bytes.Repeat([]byte{0x2A}, size)})
			if _, err := store.Encrypt("secret"); err == nil {
				t.Fatalf("Encrypt accepted a %d-byte key, want exactly 32", size)
			}
		})
	}
}

func TestFileKeySourceCreatesAndReloadsInstanceKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "integration.key")
	source := NewFileKeySource(path)

	created, err := source.Key()
	if err != nil {
		t.Fatalf("create key: %v", err)
	}
	if len(created) != 32 {
		t.Fatalf("created key length = %d, want 32", len(created))
	}

	onDisk, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read configured key path: %v", err)
	}
	if !bytes.Equal(onDisk, created) {
		t.Fatal("configured key file does not contain returned key")
	}

	reloaded, err := source.Key()
	if err != nil {
		t.Fatalf("reload key: %v", err)
	}
	if !bytes.Equal(reloaded, created) {
		t.Fatal("reloaded key differs from first-use key")
	}
}

func TestFileKeySourceCreatesOwnerOnlyKeyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "integration.key")
	if _, err := NewFileKeySource(path).Key(); err != nil {
		t.Fatalf("create key: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat key: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("key permissions = %04o, want 0600", got)
	}
}

func TestFileKeySourceCreatesIndependentRandomKeys(t *testing.T) {
	directory := t.TempDir()
	first, err := NewFileKeySource(filepath.Join(directory, "first.key")).Key()
	if err != nil {
		t.Fatalf("create first key: %v", err)
	}
	second, err := NewFileKeySource(filepath.Join(directory, "second.key")).Key()
	if err != nil {
		t.Fatalf("create second key: %v", err)
	}
	if bytes.Equal(first, second) {
		t.Fatal("independent key files contain the same generated key")
	}
}

func TestFileKeySourceConcurrentFirstUseReturnsOneKey(t *testing.T) {
	const callers = 128
	type result struct {
		key []byte
		err error
	}

	path := filepath.Join(t.TempDir(), "integration.key")
	start := make(chan struct{})
	results := make(chan result, callers)
	var group sync.WaitGroup
	for range callers {
		group.Go(func() {
			<-start
			key, err := NewFileKeySource(path).Key()
			results <- result{key: key, err: err}
		})
	}
	close(start)
	group.Wait()
	close(results)

	var expected []byte
	for result := range results {
		if result.err != nil {
			t.Fatalf("concurrent first use: %v", result.err)
		}
		if expected == nil {
			expected = result.key
			continue
		}
		if !bytes.Equal(result.key, expected) {
			t.Fatal("concurrent first use returned different keys")
		}
	}
}

func TestSecretStoreWrongKeyKeepsCiphertextUnavailable(t *testing.T) {
	writer := NewSecretStore(fixedKeySource{key: bytes.Repeat([]byte{0x11}, 32)})
	sealed, err := writer.Encrypt("tmdb-secret-value")
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	original := bytes.Clone(sealed)

	reader := NewSecretStore(fixedKeySource{key: bytes.Repeat([]byte{0x22}, 32)})
	if _, err := reader.Decrypt(sealed); !errors.Is(err, ErrCredentialUnavailable) {
		t.Fatalf("decrypt error = %v, want ErrCredentialUnavailable", err)
	}
	if !bytes.Equal(sealed, original) {
		t.Fatal("failed decrypt mutated retained ciphertext")
	}
}

func TestSecretStoreMissingKeyReportsCredentialUnavailable(t *testing.T) {
	writer := NewSecretStore(fixedKeySource{key: bytes.Repeat([]byte{0x33}, 32)})
	sealed, err := writer.Encrypt("tmdb-secret-value")
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	reader := NewSecretStore(fixedKeySource{err: os.ErrNotExist})
	if _, err := reader.Decrypt(sealed); !errors.Is(err, ErrCredentialUnavailable) {
		t.Fatalf("decrypt error = %v, want ErrCredentialUnavailable", err)
	}
}

func TestSecretStoreLostFileKeyLeavesCiphertextUnavailable(t *testing.T) {
	keyPath := filepath.Join(t.TempDir(), "integration.key")
	store := NewSecretStore(NewFileKeySource(keyPath))
	sealed, err := store.Encrypt("tmdb-secret-value")
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	original := bytes.Clone(sealed)
	if err := os.Rename(keyPath, keyPath+".lost"); err != nil {
		t.Fatalf("move instance key: %v", err)
	}

	if _, err := store.Decrypt(sealed); !errors.Is(err, ErrCredentialUnavailable) {
		t.Fatalf("decrypt after key loss = %v, want ErrCredentialUnavailable", err)
	}
	if !bytes.Equal(sealed, original) {
		t.Fatal("failed decrypt after key loss mutated retained ciphertext")
	}
}

func TestSecretStoreUsesUniqueNoncePerEncryption(t *testing.T) {
	store := NewSecretStore(fixedKeySource{key: bytes.Repeat([]byte{0x44}, 32)})

	first, err := store.Encrypt("same-tmdb-secret")
	if err != nil {
		t.Fatalf("first encrypt: %v", err)
	}
	second, err := store.Encrypt("same-tmdb-secret")
	if err != nil {
		t.Fatalf("second encrypt: %v", err)
	}
	if bytes.Equal(first, second) {
		t.Fatal("repeated encryption reused a nonce")
	}
}

func TestSecretStoreWritesAESGCMCiphertext(t *testing.T) {
	key := bytes.Repeat([]byte{0x55}, 32)
	store := NewSecretStore(fixedKeySource{key: key})
	sealed, err := store.Encrypt("tmdb-secret-value")
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatalf("build independent AES cipher: %v", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatalf("build independent GCM: %v", err)
	}
	nonceSize := gcm.NonceSize()
	plaintext, err := gcm.Open(nil, sealed[:nonceSize], sealed[nonceSize:], nil)
	if err != nil {
		t.Fatalf("open stored ciphertext with AES-GCM: %v", err)
	}
	if string(plaintext) != "tmdb-secret-value" {
		t.Fatalf("plaintext = %q, want original secret", plaintext)
	}
}

func TestSecretStoreErrorsDoNotExposeSecretFragments(t *testing.T) {
	secret := "prefix-that-must-stay-private-and-a-private-suffix"
	writer := NewSecretStore(fixedKeySource{key: bytes.Repeat([]byte{0x66}, 32)})
	sealed, err := writer.Encrypt(secret)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	reader := NewSecretStore(fixedKeySource{key: bytes.Repeat([]byte{0x77}, 32)})
	_, err = reader.Decrypt(sealed)
	if err == nil {
		t.Fatal("decrypt with wrong key succeeded")
	}
	errorText := err.Error()
	for _, fragment := range []string{
		"prefix-that-must-stay-private",
		"private-suffix",
		hex.EncodeToString(sealed[:8]),
	} {
		if strings.Contains(errorText, fragment) {
			t.Fatalf("decrypt error exposed secret fragment %q", fragment)
		}
	}
}
