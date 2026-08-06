package integration

import (
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"
)

const instanceKeySize = 32

// FileKeySource loads or creates the instance key at a configured path.
type FileKeySource struct {
	path string
}

func NewFileKeySource(path string) *FileKeySource {
	return &FileKeySource{path: path}
}

func (s *FileKeySource) Key() ([]byte, error) {
	key, err := os.ReadFile(s.path)
	if err == nil {
		return key, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}

	key = make([]byte, instanceKeySize)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	temporary, err := os.CreateTemp(filepath.Dir(s.path), "."+filepath.Base(s.path)+".tmp-")
	if err != nil {
		return nil, err
	}
	temporaryPath := temporary.Name()
	defer func() {
		_ = temporary.Close()
		_ = os.Remove(temporaryPath)
	}()
	if _, err := temporary.Write(key); err != nil {
		return nil, err
	}
	if err := temporary.Sync(); err != nil {
		return nil, err
	}
	if err := temporary.Close(); err != nil {
		return nil, err
	}

	// Linking a complete same-directory file publishes the key atomically and,
	// unlike Rename, never replaces a key created by another process.
	if err := os.Link(temporaryPath, s.path); err == nil {
		return key, nil
	} else if !errors.Is(err, os.ErrExist) {
		return nil, err
	}
	return os.ReadFile(s.path)
}
