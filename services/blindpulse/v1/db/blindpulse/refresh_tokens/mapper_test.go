package refresh_tokens

import (
	"testing"
	"time"

	"github.com/google/uuid"
	domainuser "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/user"
)

func TestMapperRoundTripIncludingNilPointers(t *testing.T) {
	now := time.Now().UTC()
	entity := domainuser.RefreshToken{ID: uuid.New(), UserID: uuid.New(), TokenHash: "hash", ExpiresAt: now.Add(time.Hour), CreatedAt: now}
	got := toDomain(fromDomain(entity))
	if got.ID != entity.ID || got.RevokedAt != nil || got.IP != nil {
		t.Fatalf("round trip = %+v", got)
	}
	revoked, ip := now, "127.0.0.1"
	entity.RevokedAt, entity.IP = &revoked, &ip
	got = toDomain(fromDomain(entity))
	if got.RevokedAt == entity.RevokedAt || got.IP == entity.IP {
		t.Fatal("pointers were aliased")
	}
}
