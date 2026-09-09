package user

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
	"github.com/jblabs/blindpulse-be/pkg/apperror"
	"github.com/jblabs/blindpulse-be/pkg/logger"
	domainuser "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/user"
)

const dummyPasswordHash = "$argon2id$v=19$m=65536,t=3,p=2$AAAAAAAAAAAAAAAAAAAAAA$AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"

func NewService(deps Dependencies) Service {
	if deps.Clock == nil {
		deps.Clock = time.Now
	}
	return &service{deps: deps}
}

func (s *service) Register(ctx context.Context, input RegisterInput) (*Session, error) {
	email := normalizeEmail(input.Email)
	if fields := ValidatePassword(input.Password); len(fields) > 0 {
		return nil, apperror.WithFields("VALIDATION_FAILED", fields)
	}
	passwordHash, err := s.deps.Hasher.Hash(input.Password)
	if err != nil {
		return nil, apperror.Wrap(err, "INTERNAL_ERROR")
	}
	name := strings.TrimSpace(input.Name)
	existing, err := s.deps.Repo.GetByEmail(ctx, email)
	var created *domainuser.User
	switch {
	case err == nil && (existing.PasswordHash != "" || existing.GoogleID != nil):
		return nil, apperror.New("EMAIL_ALREADY_REGISTERED")
	case err == nil:
		// A placeholder account (created when this email was invited to a trip before signing
		// up) has no password and no Google link yet - registering claims it in place, so every
		// participant/expense-payer reference by this user ID keeps working.
		if err := s.deps.Repo.SetPasswordHash(ctx, existing.ID, passwordHash); err != nil {
			return nil, err
		}
		existing.Name = name
		if created, err = s.deps.Repo.Update(ctx, existing); err != nil {
			return nil, err
		}
	case apperror.Is(err, "USER_NOT_FOUND"):
		created, err = s.deps.Repo.Create(ctx, &domainuser.User{ID: uuid.New(), Email: email, Name: name, PasswordHash: passwordHash})
		if err != nil {
			return nil, err
		}
	default:
		return nil, err
	}
	session, err := s.issueSession(ctx, *created)
	if err != nil {
		return nil, err
	}
	return session, nil
}

func (s *service) Authenticate(ctx context.Context, email, password string) (*Session, error) {
	found, err := s.deps.Repo.GetByEmail(ctx, normalizeEmail(email))
	missing := apperror.Is(err, "USER_NOT_FOUND")
	if err != nil && !missing {
		return nil, err
	}
	// A Google-only account has no password hash at all; treat it exactly like a missing user so
	// the failure looks identical (same dummy hash, same error) rather than a 500 from Verify("").
	noPassword := found != nil && found.PasswordHash == ""
	hash := dummyPasswordHash
	if !missing && !noPassword && found != nil {
		hash = found.PasswordHash
	}
	verified, verifyErr := s.deps.Hasher.Verify(password, hash)
	if verifyErr != nil && !missing {
		return nil, apperror.Wrap(verifyErr, "INTERNAL_ERROR")
	}
	if missing || found == nil || noPassword || !verified {
		return nil, apperror.New("INVALID_CREDENTIALS")
	}
	s.touchLastLogin(ctx, found.ID)
	return s.issueSession(ctx, *found)
}

// AuthenticateGoogle trades a verified Google ID token for a session, using the exact same
// issueSession path Authenticate uses so a Google-signed-in user gets an identical access/refresh
// pair. It resolves the user by GoogleID first, then by email (auto-linking an existing
// password account whose email matches - a Google-verified email is enough to trust the link),
// and only creates a new, password-less account when neither is found.
func (s *service) AuthenticateGoogle(ctx context.Context, idToken string) (*Session, error) {
	if s.deps.Google == nil {
		return nil, apperror.New("GOOGLE_TOKEN_INVALID")
	}
	claims, err := s.deps.Google.Verify(ctx, idToken)
	if err != nil || !claims.EmailVerified {
		return nil, apperror.New("GOOGLE_TOKEN_INVALID")
	}
	email := normalizeEmail(claims.Email)
	found, err := s.deps.Repo.GetByGoogleID(ctx, claims.Subject)
	if err == nil {
		s.touchLastLogin(ctx, found.ID)
		return s.issueSession(ctx, *found)
	}
	if !apperror.Is(err, "USER_NOT_FOUND") {
		return nil, err
	}
	found, err = s.deps.Repo.GetByEmail(ctx, email)
	if err == nil {
		if setErr := s.deps.Repo.SetGoogleID(ctx, found.ID, claims.Subject); setErr != nil {
			return nil, setErr
		}
		s.touchLastLogin(ctx, found.ID)
		return s.issueSession(ctx, *found)
	}
	if !apperror.Is(err, "USER_NOT_FOUND") {
		return nil, err
	}
	name := strings.TrimSpace(claims.Name)
	if name == "" {
		name = email
	}
	googleID := claims.Subject
	created, err := s.deps.Repo.Create(ctx, &domainuser.User{ID: uuid.New(), Email: email, Name: name, GoogleID: &googleID})
	if err != nil {
		return nil, err
	}
	s.touchLastLogin(ctx, created.ID)
	return s.issueSession(ctx, *created)
}

// touchLastLogin records a real sign-in. Failure here shouldn't fail the sign-in itself - it's a
// secondary signal (used to show "not logged in yet" in trip member lists), not a security check.
func (s *service) touchLastLogin(ctx context.Context, id uuid.UUID) {
	if err := s.deps.Repo.TouchLastLogin(ctx, id, s.deps.Clock().UTC()); err != nil {
		slog.WarnContext(ctx, "could not record last login", "user_id", id, "error", err)
	}
}

func (s *service) Refresh(ctx context.Context, raw string) (*Session, error) {
	stored, err := s.deps.Tokens.GetByHash(ctx, tokenHash(raw))
	if err != nil {
		return nil, err
	}
	if stored == nil {
		return nil, apperror.New("UNAUTHENTICATED")
	}
	if stored.RevokedAt != nil {
		if err := s.deps.Tokens.RevokeAllForUser(ctx, stored.UserID); err != nil {
			return nil, err
		}
		slog.WarnContext(ctx, "refresh token reuse detected", "user_id", stored.UserID,
			"trace_id", logger.TraceID(ctx))
		return nil, apperror.New("UNAUTHENTICATED")
	}
	if !stored.ExpiresAt.After(s.deps.Clock()) {
		if err := s.deps.Tokens.Revoke(ctx, stored.ID); err != nil {
			return nil, err
		}
		return nil, apperror.New("UNAUTHENTICATED")
	}
	if err := s.deps.Tokens.Revoke(ctx, stored.ID); err != nil {
		return nil, err
	}
	entity, err := s.deps.Repo.GetByID(ctx, stored.UserID)
	if err != nil {
		return nil, err
	}
	return s.issueSession(ctx, *entity)
}

func (s *service) Logout(ctx context.Context, raw string) error {
	stored, err := s.deps.Tokens.GetByHash(ctx, tokenHash(raw))
	if err != nil {
		return err
	}
	if stored == nil || stored.RevokedAt != nil {
		return nil
	}
	return s.deps.Tokens.Revoke(ctx, stored.ID)
}

func (s *service) GetByID(ctx context.Context, id uuid.UUID) (*domainuser.User, error) {
	return s.deps.Repo.GetByID(ctx, id)
}

func (s *service) UpdateProfile(ctx context.Context, id uuid.UUID, input UpdateProfileInput) (*domainuser.User, error) {
	entity, err := s.deps.Repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if input.Name != nil {
		entity.Name = strings.TrimSpace(*input.Name)
	}
	if input.AvatarURL != nil {
		entity.AvatarURL = input.AvatarURL
	}
	return s.deps.Repo.Update(ctx, entity)
}

func (s *service) ChangePassword(ctx context.Context, id uuid.UUID, input ChangePasswordInput) error {
	entity, err := s.deps.Repo.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if entity.PasswordHash != "" {
		verified, verifyErr := s.deps.Hasher.Verify(input.CurrentPassword, entity.PasswordHash)
		if verifyErr != nil {
			return apperror.Wrap(verifyErr, "INTERNAL_ERROR")
		}
		if !verified {
			return apperror.New("INVALID_CURRENT_PASSWORD")
		}
	}
	// entity.PasswordHash == "" means a Google-only account with no password yet - there's nothing
	// to verify against, so this call sets one for the first time instead of changing it.
	if fields := ValidatePassword(input.NewPassword); len(fields) > 0 {
		return apperror.WithFields("VALIDATION_FAILED", fields)
	}
	hash, err := s.deps.Hasher.Hash(input.NewPassword)
	if err != nil {
		return apperror.Wrap(err, "INTERNAL_ERROR")
	}
	return s.deps.Repo.SetPasswordHash(ctx, id, hash)
}

func (s *service) FindByEmail(ctx context.Context, email string) (*domainuser.User, error) {
	return s.deps.Repo.GetByEmail(ctx, normalizeEmail(email))
}

func (s *service) issueSession(ctx context.Context, entity domainuser.User) (*Session, error) {
	access, accessExpiry, err := s.deps.Issuer.IssueAccess(entity)
	if err != nil {
		return nil, apperror.Wrap(err, "INTERNAL_ERROR")
	}
	refresh, refreshExpiry, err := s.deps.Issuer.IssueRefresh(entity)
	if err != nil {
		return nil, apperror.Wrap(err, "INTERNAL_ERROR")
	}
	if err := s.deps.Tokens.Store(ctx, domainuser.RefreshToken{
		ID: uuid.New(), UserID: entity.ID, TokenHash: tokenHash(refresh), ExpiresAt: refreshExpiry,
		CreatedAt: s.deps.Clock().UTC(),
	}); err != nil {
		return nil, err
	}
	return &Session{User: entity, AccessToken: access, RefreshToken: refresh,
		AccessTokenExpiresAt: accessExpiry, RefreshTokenExpiresAt: refreshExpiry}, nil
}

func normalizeEmail(value string) string { return strings.ToLower(strings.TrimSpace(value)) }

func tokenHash(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}

// ValidatePassword enforces the app-wide password policy: at least 8 characters, with at least
// one uppercase letter, one lowercase letter, one digit, and one symbol. Shared by Register,
// CreateInvited, and ChangePassword so every path that sets a password agrees on the same rule.
func ValidatePassword(value string) []apperror.FieldError {
	var upper, lower, digit, symbol bool
	for _, character := range value {
		switch {
		case unicode.IsUpper(character):
			upper = true
		case unicode.IsLower(character):
			lower = true
		case unicode.IsDigit(character):
			digit = true
		case !unicode.IsSpace(character):
			symbol = true
		}
	}
	fields := make([]apperror.FieldError, 0, 2)
	if len(value) < 8 {
		fields = append(fields, apperror.FieldError{Field: "password", Rule: "min", Message: "password must be at least 8 characters"})
	}
	if !upper || !lower || !digit || !symbol {
		fields = append(fields, apperror.FieldError{Field: "password", Rule: "complexity", Message: "password must contain an uppercase letter, a lowercase letter, a number, and a symbol"})
	}
	return fields
}
