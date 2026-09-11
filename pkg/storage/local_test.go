package storage

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestPutAndGetRoundTrip(t *testing.T) {
	store := NewLocal(t.TempDir())
	ctx := context.Background()
	if err := store.Put(ctx, "journal/abc.png", []byte("pixels"), "image/png"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	content, contentType, err := store.Get(ctx, "journal/abc.png")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(content) != "pixels" || contentType != "image/png" {
		t.Errorf("got %q %s", content, contentType)
	}
	if err := store.Delete(ctx, "journal/abc.png"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, _, err := store.Get(ctx, "journal/abc.png"); !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v after delete, want ErrNotFound", err)
	}
	// Deleting what is already gone is not an error: the caller deleting a replaced image should
	// not fail because a previous attempt already removed it.
	if err := store.Delete(ctx, "journal/abc.png"); err != nil {
		t.Errorf("a second delete failed: %v", err)
	}
}

// The key is the only untrusted-looking input this package takes, and the defence is an allow-list
// rather than a search for "..". A pattern that admits only what the caller mints refuses the
// attacks nobody thought to enumerate, too.
func TestKeysThatEscapeTheRootAreRefused(t *testing.T) {
	root := t.TempDir()
	store := NewLocal(root)
	ctx := context.Background()

	// A file to steal, outside the root, so a successful traversal would be demonstrable rather
	// than theoretical.
	secret := filepath.Join(filepath.Dir(root), "secret.txt")
	if err := os.WriteFile(secret, []byte("not yours"), 0o600); err != nil {
		t.Fatalf("fixture: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(secret) })

	escapes := []string{
		"../secret.txt",
		"journal/../../secret.txt",
		"/etc/passwd",
		"..",
		"./journal/x.png",
		"journal//x.png",
		"journal/x.png\x00.txt",
		"C:\\windows\\system32",
		"",
		"a/b/c/d/e/f.png", // deeper than the pattern allows
	}
	for _, key := range escapes {
		if err := store.Put(ctx, key, []byte("x"), "image/png"); !errors.Is(err, ErrInvalidKey) {
			t.Errorf("Put(%q) err = %v, want ErrInvalidKey", key, err)
		}
		if _, _, err := store.Get(ctx, key); !errors.Is(err, ErrInvalidKey) {
			t.Errorf("Get(%q) err = %v, want ErrInvalidKey", key, err)
		}
		if err := store.Delete(ctx, key); !errors.Is(err, ErrInvalidKey) {
			t.Errorf("Delete(%q) err = %v, want ErrInvalidKey", key, err)
		}
	}
	// And nothing escaped: the fixture is untouched.
	if content, err := os.ReadFile(secret); err != nil || string(content) != "not yours" {
		t.Errorf("the file outside the root was reached: %q %v", content, err)
	}
}

func TestPutLeavesNoPartialFileBehind(t *testing.T) {
	root := t.TempDir()
	store := NewLocal(root)
	if err := store.Put(context.Background(), "journal/abc.png", []byte("pixels"), "image/png"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	// The write goes to a temporary name and is renamed, so a reader never sees a half-written
	// image. What this checks is the other half of that: the temporary name does not survive.
	entries, err := os.ReadDir(filepath.Join(root, "journal"))
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	for _, entry := range entries {
		if entry.Name() != "abc.png" {
			t.Errorf("a stray file survived the write: %s", entry.Name())
		}
	}
}

func TestReplacingAnObjectOverwritesIt(t *testing.T) {
	store := NewLocal(t.TempDir())
	ctx := context.Background()
	if err := store.Put(ctx, "journal/abc.png", []byte("first"), "image/png"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := store.Put(ctx, "journal/abc.png", []byte("second"), "image/png"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	content, _, err := store.Get(ctx, "journal/abc.png")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(content) != "second" {
		t.Errorf("content = %q, want the replacement", content)
	}
}
