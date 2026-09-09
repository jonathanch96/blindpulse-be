// Package google verifies Google "Sign in with Google" ID tokens without pulling in the full
// google-api-go-client dependency tree: it fetches Google's published JWKS, checks the RS256
// signature, issuer, audience, and expiry using the same golang-jwt library the rest of this
// service already depends on for its own tokens.
package google

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"sync"
	"time"

	jwtlib "github.com/golang-jwt/jwt/v5"
)

const certsURL = "https://www.googleapis.com/oauth2/v3/certs"

var validIssuers = map[string]struct{}{
	"accounts.google.com":         {},
	"https://accounts.google.com": {},
}

// Claims is the subset of a verified Google ID token this service needs.
type Claims struct {
	Subject       string
	Email         string
	EmailVerified bool
	Name          string
}

type jwk struct {
	Kid string `json:"kid"`
	N   string `json:"n"`
	E   string `json:"e"`
}

type jwks struct {
	Keys []jwk `json:"keys"`
}

type idTokenClaims struct {
	jwtlib.RegisteredClaims
	Email         string `json:"email"`
	EmailVerified bool   `json:"email_verified"`
	Name          string `json:"name"`
}

// Verifier checks Google ID tokens against a given OAuth client ID (the expected audience).
type Verifier struct {
	audience string
	client   *http.Client

	mu        sync.Mutex
	keys      map[string]*rsa.PublicKey
	fetchedAt time.Time
}

func NewVerifier(audience string) *Verifier {
	return &Verifier{audience: audience, client: &http.Client{Timeout: 10 * time.Second}}
}

// Verify parses and validates a Google-issued ID token, returning the identity it asserts.
func (v *Verifier) Verify(ctx context.Context, rawToken string) (*Claims, error) {
	claims := new(idTokenClaims)
	token, err := jwtlib.ParseWithClaims(rawToken, claims, func(token *jwtlib.Token) (any, error) {
		kid, _ := token.Header["kid"].(string)
		if kid == "" {
			return nil, errors.New("google id token is missing a key id")
		}
		return v.publicKey(ctx, kid)
	}, jwtlib.WithValidMethods([]string{jwtlib.SigningMethodRS256.Alg()}), jwtlib.WithExpirationRequired(),
		jwtlib.WithAudience(v.audience))
	if err != nil || !token.Valid {
		return nil, fmt.Errorf("verify google id token: %w", err)
	}
	if _, ok := validIssuers[claims.Issuer]; !ok {
		return nil, fmt.Errorf("unexpected google id token issuer %q", claims.Issuer)
	}
	if claims.Subject == "" || claims.Email == "" {
		return nil, errors.New("google id token is missing subject or email")
	}
	return &Claims{Subject: claims.Subject, Email: claims.Email, EmailVerified: claims.EmailVerified, Name: claims.Name}, nil
}

func (v *Verifier) publicKey(ctx context.Context, kid string) (*rsa.PublicKey, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if key, ok := v.keys[kid]; ok && time.Since(v.fetchedAt) < time.Hour {
		return key, nil
	}
	keys, err := v.fetchKeys(ctx)
	if err != nil {
		return nil, err
	}
	v.keys, v.fetchedAt = keys, time.Now()
	key, ok := keys[kid]
	if !ok {
		return nil, fmt.Errorf("no google signing key found for kid %q", kid)
	}
	return key, nil
}

func (v *Verifier) fetchKeys(ctx context.Context) (map[string]*rsa.PublicKey, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, certsURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := v.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch google jwks: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch google jwks: unexpected status %d", resp.StatusCode)
	}
	var body jwks
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("decode google jwks: %w", err)
	}
	result := make(map[string]*rsa.PublicKey, len(body.Keys))
	for _, key := range body.Keys {
		public, err := rsaPublicKey(key.N, key.E)
		if err != nil {
			continue
		}
		result[key.Kid] = public
	}
	return result, nil
}

func rsaPublicKey(nRaw, eRaw string) (*rsa.PublicKey, error) {
	nBytes, err := base64.RawURLEncoding.DecodeString(nRaw)
	if err != nil {
		return nil, err
	}
	eBytes, err := base64.RawURLEncoding.DecodeString(eRaw)
	if err != nil {
		return nil, err
	}
	return &rsa.PublicKey{N: new(big.Int).SetBytes(nBytes), E: int(new(big.Int).SetBytes(eBytes).Int64())}, nil
}
