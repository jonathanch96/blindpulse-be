package users

import (
	"testing"
	"time"

	"github.com/google/uuid"
	domainuser "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/user"
)

func TestMapperRoundTripIncludingNilPointers(t *testing.T) {
	now := time.Now().UTC()
	entity := domainuser.User{ID: uuid.New(), Email: "a@example.com", Name: "A", PasswordHash: "secret", CreatedAt: now, UpdatedAt: now}
	if got := toDomain(fromDomain(entity)); got != entity {
		t.Fatalf("round trip = %+v", got)
	}
	avatar := "https://example.com/a.png"
	entity.AvatarURL = &avatar
	got := toDomain(fromDomain(entity))
	if got.AvatarURL == entity.AvatarURL || *got.AvatarURL != avatar {
		t.Fatalf("pointer was not safely mapped")
	}
}
