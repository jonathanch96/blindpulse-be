package refresh_tokens

import (
	"time"

	domainuser "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/user"
)

func toDomain(model RefreshToken) domainuser.RefreshToken {
	return domainuser.RefreshToken{
		ID: model.ID, UserID: model.UserID, TokenHash: model.TokenHash, ExpiresAt: model.ExpiresAt,
		RevokedAt: cloneTime(model.RevokedAt), UserAgent: cloneString(model.UserAgent),
		IP: cloneString(model.IP), CreatedAt: model.CreatedAt,
	}
}

func fromDomain(entity domainuser.RefreshToken) RefreshToken {
	return RefreshToken{
		ID: entity.ID, UserID: entity.UserID, TokenHash: entity.TokenHash, ExpiresAt: entity.ExpiresAt,
		RevokedAt: cloneTime(entity.RevokedAt), UserAgent: cloneString(entity.UserAgent),
		IP: cloneString(entity.IP), CreatedAt: entity.CreatedAt,
	}
}

func cloneString(value *string) *string {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func cloneTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}
