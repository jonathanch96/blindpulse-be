package user

import (
	"time"

	"github.com/google/uuid"
)

type User struct {
	ID    uuid.UUID
	Email string
	Name  string
	// PasswordHash is empty for an account created via Google sign-in.
	PasswordHash string
	// GoogleID is the Google account's stable subject ("sub") claim, set once a user signs in
	// with Google (either at account creation or by linking an existing password account).
	GoogleID *string
	// LastLoginAt is set the first time this user actually signs in (password or Google) - unlike
	// PasswordHash/GoogleID, which can be set on their behalf (e.g. an invite creating an account
	// with a chosen password), this only ever changes when they use it themselves.
	LastLoginAt *time.Time
	AvatarURL   *string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type PublicUser struct {
	ID        uuid.UUID
	Email     string
	Name      string
	AvatarURL *string
	// HasAccount is true once a user has credentials of any kind (password or Google). Invited
	// accounts get a password immediately (CreateInvited), so this is nearly always true - use
	// HasLoggedIn to tell whether they've actually signed in.
	HasAccount bool
	// HasLoggedIn is true once this user has signed in at least once (password or Google). An
	// invited account has credentials (HasAccount) from the moment it's created, but stays
	// HasLoggedIn=false until the invitee actually uses them - that's the signal trip member lists
	// use to show "not logged in yet".
	HasLoggedIn bool
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func (u User) Public() PublicUser {
	return PublicUser{
		ID: u.ID, Email: u.Email, Name: u.Name, AvatarURL: u.AvatarURL,
		HasAccount:  u.PasswordHash != "" || u.GoogleID != nil,
		HasLoggedIn: u.LastLoginAt != nil,
		CreatedAt:   u.CreatedAt, UpdatedAt: u.UpdatedAt,
	}
}

type RefreshToken struct {
	ID        uuid.UUID
	UserID    uuid.UUID
	TokenHash string
	ExpiresAt time.Time
	RevokedAt *time.Time
	UserAgent *string
	IP        *string
	CreatedAt time.Time
}
