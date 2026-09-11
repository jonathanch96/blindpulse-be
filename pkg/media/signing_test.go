package media

import (
	"net/url"
	"strings"
	"testing"
	"time"
)

func signerAt(secret string, ttl time.Duration, now time.Time) *Signer {
	signer := NewSigner(secret, ttl)
	signer.now = func() time.Time { return now }
	return signer
}

func parts(t *testing.T, query string) (string, string) {
	t.Helper()
	values, err := url.ParseQuery(query)
	if err != nil {
		t.Fatalf("parse %q: %v", query, err)
	}
	return values.Get("exp"), values.Get("sig")
}

func TestASignedLinkVerifiesAndThenExpires(t *testing.T) {
	issued := time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)
	signer := signerAt("a-real-secret", 15*time.Minute, issued)
	expires, signature := parts(t, signer.Sign("journal/abc.png"))

	if err := signer.Verify("journal/abc.png", expires, signature); err != nil {
		t.Fatalf("a freshly signed link was refused: %v", err)
	}

	later := signerAt("a-real-secret", 15*time.Minute, issued.Add(16*time.Minute))
	if err := later.Verify("journal/abc.png", expires, signature); err != ErrLinkExpired {
		t.Errorf("err = %v, want ErrLinkExpired", err)
	}
}

// The link is the authority — an <img src> cannot carry a bearer — so everything about it has to be
// unforgeable on its own.
func TestAForgedLinkIsRefused(t *testing.T) {
	issued := time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)
	signer := signerAt("a-real-secret", 15*time.Minute, issued)
	expires, signature := parts(t, signer.Sign("journal/abc.png"))

	cases := map[string][3]string{
		"another key with this signature": {"journal/someone-elses.png", expires, signature},
		"a later expiry, same signature":  {"journal/abc.png", "99999999999", signature},
		"a mangled signature":             {"journal/abc.png", expires, signature[:len(signature)-1] + "A"},
		"no signature at all":             {"journal/abc.png", expires, ""},
		"a non-numeric expiry":            {"journal/abc.png", "soon", signature},
	}
	for name, args := range cases {
		if err := signer.Verify(args[0], args[1], args[2]); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}

	// A different secret must not validate the same link, or rotating the key would change nothing.
	other := signerAt("a-different-secret", 15*time.Minute, issued)
	if err := other.Verify("journal/abc.png", expires, signature); err == nil {
		t.Error("a link signed with one secret verified under another")
	}
}

// Expiry is checked after the signature on purpose: answering "expired" to an unsigned guess would
// confirm that the key exists, which is the one thing an attacker without a link wants to know.
func TestAnExpiredForgeryReadsAsInvalidRatherThanExpired(t *testing.T) {
	issued := time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)
	signer := signerAt("a-real-secret", time.Minute, issued.Add(time.Hour))
	if err := signer.Verify("journal/abc.png", "1000000000", "not-a-signature"); err != ErrLinkInvalid {
		t.Errorf("err = %v, want ErrLinkInvalid — an expired-looking forgery must not confirm the key", err)
	}
}

// The separator between the key and the expiry stops two different links sharing one signature.
func TestKeyAndExpiryCannotBeConfusedWithEachOther(t *testing.T) {
	issued := time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)
	signer := signerAt("a-real-secret", 15*time.Minute, issued)
	first, _ := parts(t, signer.Sign("a/b"))
	_, signature := parts(t, signer.Sign("a"))
	if err := signer.Verify("a/b", first, signature); err == nil {
		t.Error(`signing "a" produced a signature that validates "a/b"`)
	}
}

// An empty secret disables the feature rather than signing with a known key: a predictable
// signature is worse than no feature, because it looks like protection.
func TestNoSecretMeansNoLinks(t *testing.T) {
	signer := NewSigner("", 15*time.Minute)
	if signer.Enabled() {
		t.Error("a signer with no secret reports itself enabled")
	}
	if got := signer.Sign("journal/abc.png"); got != "" {
		t.Errorf("Sign returned %q with no secret", got)
	}
	if got := signer.URL("http://localhost/media", "journal/abc.png"); got != "" {
		t.Errorf("URL returned %q with no secret", got)
	}
	if err := signer.Verify("journal/abc.png", "1", "x"); err == nil {
		t.Error("a disabled signer verified a link")
	}
}

func TestURLEscapesTheKeyAndCarriesTheQuery(t *testing.T) {
	signer := signerAt("a-real-secret", 15*time.Minute, time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC))
	got := signer.URL("http://localhost:8080/api/v1/journal-media/", "journal/abc def.png")
	if strings.Contains(got, " ") {
		t.Errorf("URL left a raw space in %q", got)
	}
	if !strings.Contains(got, "exp=") || !strings.Contains(got, "sig=") {
		t.Errorf("URL is missing its query: %q", got)
	}
	// The trailing slash on the base must not double up.
	if strings.Contains(got, "journal-media//") {
		t.Errorf("URL doubled the separator: %q", got)
	}
	// The slashes *inside* the key must survive as slashes. Escaping the key whole produces %2F,
	// which Go normalizes back to "/" before routing — so the link looks right, routes nowhere, and
	// every image 404s while the signature is perfectly valid. That is exactly what happened.
	if strings.Contains(got, "%2F") {
		t.Errorf("URL escaped the key's slashes: %q", got)
	}
	if !strings.Contains(got, "/journal/abc%20def.png") {
		t.Errorf("URL did not keep the key's path shape: %q", got)
	}
}
