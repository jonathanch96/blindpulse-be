// Package storage holds uploaded files.
//
// One interface with one implementation today. S3/MinIO is what the StorageConfig's Endpoint field
// anticipates and is deliberately not written here: it cannot be exercised from this environment —
// the network policy blocks every non-GitHub host — and an object-store adapter that has never
// talked to an object store is not a feature, it is a claim. The interface is the part that makes
// adding it a new file rather than a rewrite.
package storage

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Store is a content-addressed blob store keyed by an opaque string.
type Store interface {
	Put(ctx context.Context, key string, content []byte, contentType string) error
	Get(ctx context.Context, key string) ([]byte, string, error)
	Delete(ctx context.Context, key string) error
}

var (
	ErrNotFound   = errors.New("storage: no object with that key")
	ErrInvalidKey = errors.New("storage: key is not in the accepted form")
)

// keyPattern is the whole defence against path traversal, and it is an allow-list rather than a
// check for "..". A key is segments of hex, dashes and dots — which is what the caller mints — so
// "../../etc/passwd", an absolute path, a NUL byte and a Windows drive letter all fail by not
// matching, rather than by being individually anticipated.
var keyPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,127}(/[a-zA-Z0-9][a-zA-Z0-9._-]{0,127}){0,3}$`)

type localStore struct{ root string }

// NewLocal stores objects under root. The directory is created on first write rather than at
// construction, so a deployment that never accepts an upload never makes a directory for one.
func NewLocal(root string) Store { return &localStore{root: root} }

func (s *localStore) Put(_ context.Context, key string, content []byte, _ string) error {
	path, err := s.path(key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("storage: create directory: %w", err)
	}
	// Written to a temporary name and renamed, so a reader can never see a half-written image. On
	// the same filesystem the rename is atomic; a torn file served under a signed URL would be a
	// broken image the trader could not explain.
	temp, err := os.CreateTemp(filepath.Dir(path), ".upload-*")
	if err != nil {
		return fmt.Errorf("storage: create temp: %w", err)
	}
	tempName := temp.Name()
	defer func() {
		// Removing a name that has already been renamed away is a no-op; this only bites on the
		// failure paths, where it stops a failed upload from leaving a file behind forever.
		_ = os.Remove(tempName)
	}()
	if _, err := temp.Write(content); err != nil {
		_ = temp.Close()
		return fmt.Errorf("storage: write: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("storage: close: %w", err)
	}
	if err := os.Chmod(tempName, 0o640); err != nil {
		return fmt.Errorf("storage: chmod: %w", err)
	}
	if err := os.Rename(tempName, path); err != nil {
		return fmt.Errorf("storage: rename: %w", err)
	}
	return nil
}

func (s *localStore) Get(_ context.Context, key string) ([]byte, string, error) {
	path, err := s.path(key)
	if err != nil {
		return nil, "", err
	}
	content, err := os.ReadFile(path) //nolint:gosec // the key is allow-listed and rooted above
	if errors.Is(err, os.ErrNotExist) {
		return nil, "", ErrNotFound
	}
	if err != nil {
		return nil, "", fmt.Errorf("storage: read: %w", err)
	}
	return content, contentTypeFor(key), nil
}

func (s *localStore) Delete(_ context.Context, key string) error {
	path, err := s.path(key)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("storage: delete: %w", err)
	}
	return nil
}

// path validates the key and then proves the result is still under the root. The pattern should
// make the second check redundant; it is here anyway because "should be redundant" is what every
// path traversal was before it was found, and the cost is one string comparison.
func (s *localStore) path(key string) (string, error) {
	if !keyPattern.MatchString(key) {
		return "", ErrInvalidKey
	}
	root, err := filepath.Abs(s.root)
	if err != nil {
		return "", fmt.Errorf("storage: resolve root: %w", err)
	}
	full := filepath.Join(root, filepath.FromSlash(key))
	if full != root && !strings.HasPrefix(full, root+string(os.PathSeparator)) {
		return "", ErrInvalidKey
	}
	return full, nil
}

// contentTypeFor reads the extension the writer chose, which is derived from the sniffed format
// rather than from anything the uploader sent.
func contentTypeFor(key string) string {
	if strings.HasSuffix(key, ".jpg") {
		return "image/jpeg"
	}
	return "image/png"
}
