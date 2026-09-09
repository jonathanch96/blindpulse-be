package jwt

import (
	"strings"
	"testing"
	"time"

	jwtlib "github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	domainuser "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/user"
)

func testIssuer(now time.Time) *Issuer {
	issuer := NewIssuer(Config{AccessSecret: strings.Repeat("a", 32), RefreshSecret: strings.Repeat("b", 32), AccessTTL: 15 * time.Minute, RefreshTTL: 30 * 24 * time.Hour})
	issuer.clock = func() time.Time { return now }
	return issuer
}

func TestAccessTokenIssueParseAndExpiry(t *testing.T) {
	now := time.Date(2026, 8, 5, 1, 2, 3, 0, time.UTC)
	issuer := testIssuer(now)
	entity := domainuser.User{ID: uuid.New(), Email: "person@example.com", Name: "Person"}
	raw, expires, err := issuer.IssueAccess(entity)
	if err != nil {
		t.Fatal(err)
	}
	if !expires.Equal(now.Add(15 * time.Minute)) {
		t.Fatalf("expiry = %v", expires)
	}
	claims, err := issuer.ParseAccess(raw)
	if err != nil || claims.Subject != entity.ID.String() || claims.Email != entity.Email {
		t.Fatalf("claims = %+v, %v", claims, err)
	}
	issuer.clock = func() time.Time { return expires.Add(time.Second) }
	if _, err := issuer.ParseAccess(raw); err == nil {
		t.Fatal("expired token accepted")
	}
}

func TestAccessTokenRejectsWrongSecretTamperingAndAlgNone(t *testing.T) {
	now := time.Now().UTC()
	issuer := testIssuer(now)
	entity := domainuser.User{ID: uuid.New(), Email: "person@example.com", Name: "Person"}
	raw, _, _ := issuer.IssueAccess(entity)
	wrong := testIssuer(now)
	wrong.accessSecret = []byte(strings.Repeat("z", 32))
	if _, err := wrong.ParseAccess(raw); err == nil {
		t.Fatal("wrong secret accepted")
	}
	parts := strings.Split(raw, ".")
	parts[1] = parts[1][:len(parts[1])-1] + "A"
	if _, err := issuer.ParseAccess(strings.Join(parts, ".")); err == nil {
		t.Fatal("tampered token accepted")
	}
	claims := Claims{RegisteredClaims: jwtlib.RegisteredClaims{Issuer: "blindpulse", Subject: entity.ID.String(), ExpiresAt: jwtlib.NewNumericDate(now.Add(time.Hour))}}
	none, _ := jwtlib.NewWithClaims(jwtlib.SigningMethodNone, claims).SignedString(jwtlib.UnsafeAllowNoneSignatureType)
	if _, err := issuer.ParseAccess(none); err == nil {
		t.Fatal("alg none accepted")
	}
}

func TestAccessTokenRejectsMissingSubject(t *testing.T) {
	issuer := testIssuer(time.Now().UTC())
	claims := Claims{RegisteredClaims: jwtlib.RegisteredClaims{Issuer: "blindpulse", ExpiresAt: jwtlib.NewNumericDate(issuer.clock().Add(time.Hour)), IssuedAt: jwtlib.NewNumericDate(issuer.clock())}}
	raw, _ := jwtlib.NewWithClaims(jwtlib.SigningMethodHS256, claims).SignedString(issuer.accessSecret)
	if _, err := issuer.ParseAccess(raw); err == nil {
		t.Fatal("missing subject accepted")
	}
}

func TestRefreshTokensAreOpaqueAndUnique(t *testing.T) {
	issuer := testIssuer(time.Now().UTC())
	a, expiry, err := issuer.IssueRefresh(domainuser.User{})
	if err != nil {
		t.Fatal(err)
	}
	b, _, _ := issuer.IssueRefresh(domainuser.User{})
	if a == b || strings.Count(a, ".") != 0 || !expiry.Equal(issuer.clock().Add(30*24*time.Hour)) {
		t.Fatalf("invalid refresh tokens")
	}
}
