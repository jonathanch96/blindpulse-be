package media

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Signer mints and checks time-limited links to stored media.
//
// The alternative — serving media behind the bearer middleware — does not work: an <img src> cannot
// carry an Authorization header, which is the same constraint that produced the websocket's
// single-use ticket. So the link itself carries the authority, and it expires.
//
// What a signed link is *not* is an access control decision. It says "this key was released by this
// server before this instant", not "the bearer owns it". The ownership check happens when the link
// is minted, on a journal entry the caller has already proved is theirs.
type Signer struct {
	secret []byte
	ttl    time.Duration
	now    func() time.Time
}

var (
	ErrLinkExpired = errors.New("media: this link has expired")
	ErrLinkInvalid = errors.New("media: this link is not valid")
)

func NewSigner(secret string, ttl time.Duration) *Signer {
	if ttl <= 0 {
		ttl = 15 * time.Minute
	}
	return &Signer{secret: []byte(secret), ttl: ttl, now: func() time.Time { return time.Now().UTC() }}
}

// Enabled reports whether links can be signed at all. An empty secret disables media rather than
// signing with a known key: a predictable signature is worse than no feature, because it looks like
// protection.
func (s *Signer) Enabled() bool { return s != nil && len(s.secret) > 0 }

// Sign returns the query string granting temporary access to one key.
func (s *Signer) Sign(key string) string {
	if !s.Enabled() {
		return ""
	}
	expires := s.now().Add(s.ttl).Unix()
	return fmt.Sprintf("exp=%d&sig=%s", expires, s.signature(key, expires))
}

// Verify checks a link and reports why it failed, because "expired" and "forged" are different
// facts: the first is ordinary and the second is worth noticing.
func (s *Signer) Verify(key, expires, signature string) error {
	if !s.Enabled() {
		return ErrLinkInvalid
	}
	at, err := strconv.ParseInt(expires, 10, 64)
	if err != nil {
		return ErrLinkInvalid
	}
	// Compared with hmac.Equal rather than ==, so the comparison takes the same time whatever the
	// first differing byte is. A string compare on a signature leaks its prefix to anyone willing
	// to measure, which is how a forgery gets built one byte at a time.
	if !hmac.Equal([]byte(signature), []byte(s.signature(key, at))) {
		return ErrLinkInvalid
	}
	// Expiry is checked *after* the signature. Answering "expired" for an unsigned guess would
	// confirm that the key exists.
	if s.now().Unix() > at {
		return ErrLinkExpired
	}
	return nil
}

func (s *Signer) signature(key string, expires int64) string {
	mac := hmac.New(sha256.New, s.secret)
	// The separator is a byte that cannot appear in a key, so ("a/b", 1) and ("a", "/b1") cannot
	// produce the same input. Without it, two different links could share a signature.
	mac.Write([]byte(key))
	mac.Write([]byte{0})
	mac.Write([]byte(strconv.FormatInt(expires, 10)))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// URL builds the full link a client can put in an <img src>.
//
// Each segment is escaped separately, so the slashes in a key stay slashes. Escaping the key whole
// turns them into %2F, which Go normalizes back to "/" before routing — the link would look correct
// and route nowhere.
func (s *Signer) URL(base, key string) string {
	query := s.Sign(key)
	if query == "" {
		return ""
	}
	segments := strings.Split(key, "/")
	for i, segment := range segments {
		segments[i] = url.PathEscape(segment)
	}
	return fmt.Sprintf("%s/%s?%s", strings.TrimRight(base, "/"), strings.Join(segments, "/"), query)
}
