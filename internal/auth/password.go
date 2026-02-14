package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"
)

const (
	argonTime    uint32 = 3
	argonMemory  uint32 = 64 * 1024
	argonThreads uint8  = 2
	argonKeyLen  uint32 = 32
	saltLen             = 16
)

func hashPassword(password string) (string, error) {
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}

	hash := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)

	saltEncoded := base64.RawStdEncoding.EncodeToString(salt)
	hashEncoded := base64.RawStdEncoding.EncodeToString(hash)

	return fmt.Sprintf(
		"argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version,
		argonMemory,
		argonTime,
		argonThreads,
		saltEncoded,
		hashEncoded,
	), nil
}

func verifyPassword(password, encoded string) (bool, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 5 {
		return false, fmt.Errorf("invalid hash format")
	}
	if parts[0] != "argon2id" {
		return false, fmt.Errorf("unsupported algorithm")
	}
	if parts[1] != "v=19" {
		return false, fmt.Errorf("unsupported hash version")
	}

	params := strings.Split(parts[2], ",")
	if len(params) != 3 {
		return false, fmt.Errorf("invalid params")
	}

	memory, err := parseCostParam(params[0], "m")
	if err != nil {
		return false, err
	}
	timeCost, err := parseCostParam(params[1], "t")
	if err != nil {
		return false, err
	}
	threads, err := parseCostParam(params[2], "p")
	if err != nil {
		return false, err
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[3])
	if err != nil {
		return false, err
	}
	expectedHash, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false, err
	}

	actualHash := argon2.IDKey([]byte(password), salt, uint32(timeCost), uint32(memory), uint8(threads), uint32(len(expectedHash)))
	return subtle.ConstantTimeCompare(actualHash, expectedHash) == 1, nil
}

func parseCostParam(param, key string) (int, error) {
	trimmed := strings.TrimPrefix(param, key+"=")
	if trimmed == param {
		return 0, fmt.Errorf("missing %s param", key)
	}

	value, err := strconv.Atoi(trimmed)
	if err != nil {
		return 0, err
	}
	if value <= 0 {
		return 0, fmt.Errorf("invalid %s param", key)
	}

	return value, nil
}
