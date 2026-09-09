package userresponse

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	domainuser "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/user"
)

func TestResponseNeverContainsPasswordHash(t *testing.T) {
	encoded, err := json.Marshal(FromDomain(domainuser.User{ID: uuid.New(), Email: "a@example.com", Name: "A", PasswordHash: "super-secret-hash"}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "password") || strings.Contains(string(encoded), "super-secret-hash") {
		t.Fatalf("leaked response: %s", encoded)
	}
}
