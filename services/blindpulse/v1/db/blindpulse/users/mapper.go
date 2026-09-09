package users

import (
	"time"

	domainuser "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/user"
)

func toDomain(model User) domainuser.User {
	return domainuser.User{
		ID: model.ID, Email: model.Email, Name: model.Name, PasswordHash: model.PasswordHash, GoogleID: cloneString(model.GoogleID),
		LastLoginAt: cloneTime(model.LastLoginAt),
		AvatarURL:   cloneString(model.AvatarURL), CreatedAt: model.CreatedAt, UpdatedAt: model.UpdatedAt,
	}
}

func fromDomain(entity domainuser.User) User {
	return User{
		ID: entity.ID, Email: entity.Email, Name: entity.Name, PasswordHash: entity.PasswordHash, GoogleID: cloneString(entity.GoogleID),
		LastLoginAt: cloneTime(entity.LastLoginAt),
		AvatarURL:   cloneString(entity.AvatarURL), CreatedAt: entity.CreatedAt, UpdatedAt: entity.UpdatedAt,
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
