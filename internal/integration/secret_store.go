package integration

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
)

// ErrCredentialUnavailable means retained ciphertext cannot be decrypted with
// the current instance key.
var ErrCredentialUnavailable = errors.New("integration credential unavailable")

// KeySource supplies the instance key used for persisted integration secrets.
type KeySource interface {
	Key() ([]byte, error)
}

// SecretStore encrypts persisted integration credentials.
type SecretStore struct {
	keys KeySource
}

func NewSecretStore(keys KeySource) *SecretStore {
	return &SecretStore{keys: keys}
}

func (s *SecretStore) Encrypt(secret string) ([]byte, error) {
	aead, err := s.aead()
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return aead.Seal(nonce, nonce, []byte(secret), nil), nil
}

func (s *SecretStore) Decrypt(sealed []byte) (string, error) {
	aead, err := s.aead()
	if err != nil {
		return "", ErrCredentialUnavailable
	}
	nonceSize := aead.NonceSize()
	if len(sealed) < nonceSize {
		return "", errors.New("integration credential ciphertext too short")
	}
	plaintext, err := aead.Open(nil, sealed[:nonceSize], sealed[nonceSize:], nil)
	if err != nil {
		return "", ErrCredentialUnavailable
	}
	return string(plaintext), nil
}

func (s *SecretStore) aead() (cipher.AEAD, error) {
	key, err := s.keys.Key()
	if err != nil {
		return nil, err
	}
	if len(key) != 32 {
		return nil, errors.New("integration instance key must be 32 bytes")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}
