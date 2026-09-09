package authresponse

import (
	"time"

	userdomain "github.com/jblabs/blindpulse-be/services/blindpulse/v1/domain/user"
	userresponse "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/response/user"
)

type Session struct {
	User                  userresponse.User `json:"user"`
	AccessToken           string            `json:"access_token"`
	RefreshToken          string            `json:"refresh_token"`
	AccessTokenExpiresAt  time.Time         `json:"access_token_expires_at"`
	RefreshTokenExpiresAt time.Time         `json:"refresh_token_expires_at"`
}

func FromSession(session *userdomain.Session) Session {
	return Session{User: userresponse.FromDomain(session.User), AccessToken: session.AccessToken,
		RefreshToken: session.RefreshToken, AccessTokenExpiresAt: session.AccessTokenExpiresAt,
		RefreshTokenExpiresAt: session.RefreshTokenExpiresAt}
}
