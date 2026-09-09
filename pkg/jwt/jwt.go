package jwt

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	jwtlib "github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	domainuser "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/user"
)

type Claims struct {
	jwtlib.RegisteredClaims
	Email string `json:"email"`
	Name  string `json:"name"`
}

type Issuer struct {
	accessSecret  []byte
	refreshSecret []byte
	accessTTL     time.Duration
	refreshTTL    time.Duration
	clock         func() time.Time
}

type Config struct {
	AccessSecret  string
	RefreshSecret string
	AccessTTL     time.Duration
	RefreshTTL    time.Duration
}

func NewIssuer(cfg Config) *Issuer {
	return &Issuer{accessSecret: []byte(cfg.AccessSecret), refreshSecret: []byte(cfg.RefreshSecret),
		accessTTL: cfg.AccessTTL, refreshTTL: cfg.RefreshTTL, clock: time.Now}
}

func (i *Issuer) IssueAccess(u domainuser.User) (string, time.Time, error) {
	now := i.clock().UTC()
	expiresAt := now.Add(i.accessTTL)
	claims := Claims{RegisteredClaims: jwtlib.RegisteredClaims{
		Issuer: "blindpulse", Subject: u.ID.String(), ID: uuid.NewString(),
		IssuedAt: jwtlib.NewNumericDate(now), ExpiresAt: jwtlib.NewNumericDate(expiresAt),
	}, Email: u.Email, Name: u.Name}
	token := jwtlib.NewWithClaims(jwtlib.SigningMethodHS256, claims)
	signed, err := token.SignedString(i.accessSecret)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("sign access token: %w", err)
	}
	return signed, expiresAt, nil
}

func (i *Issuer) IssueRefresh(_ domainuser.User) (string, time.Time, error) {
	random := make([]byte, 32)
	if _, err := rand.Read(random); err != nil {
		return "", time.Time{}, fmt.Errorf("generate refresh token: %w", err)
	}
	mac := hmac.New(sha256.New, i.refreshSecret)
	_, _ = mac.Write(random)
	token := append(random, mac.Sum(nil)...)
	return base64.RawURLEncoding.EncodeToString(token), i.clock().UTC().Add(i.refreshTTL), nil
}

func (i *Issuer) ParseAccess(raw string) (*Claims, error) {
	claims := new(Claims)
	token, err := jwtlib.ParseWithClaims(raw, claims, func(token *jwtlib.Token) (any, error) {
		if token.Method != jwtlib.SigningMethodHS256 || token.Method.Alg() != jwtlib.SigningMethodHS256.Alg() {
			return nil, errors.New("access token must use HS256")
		}
		return i.accessSecret, nil
	}, jwtlib.WithIssuer("blindpulse"), jwtlib.WithExpirationRequired(), jwtlib.WithIssuedAt(),
		jwtlib.WithValidMethods([]string{jwtlib.SigningMethodHS256.Alg()}), jwtlib.WithTimeFunc(i.clock))
	if err != nil || !token.Valid {
		return nil, fmt.Errorf("parse access token: %w", err)
	}
	if claims.Subject == "" {
		return nil, errors.New("access token subject is required")
	}
	if _, err := uuid.Parse(claims.Subject); err != nil {
		return nil, errors.New("access token subject is invalid")
	}
	return claims, nil
}
