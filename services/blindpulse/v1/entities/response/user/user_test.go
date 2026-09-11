package userresponse

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	domainuser "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/user"
)

func sampleUser() domainuser.User {
	googleID := "google-subject-117"
	lastLogin := time.Date(2026, 2, 3, 9, 30, 0, 0, time.UTC)
	return domainuser.User{
		ID:           uuid.MustParse("4f0b2d1a-7c58-4a9e-9b31-2e6a8c5d0f77"),
		Email:        "trader@example.com",
		Name:         "Trader",
		PasswordHash: "$argon2id$v=19$m=65536,t=3,p=4$c29tZXNhbHQ$aGFzaC1ieXRlcw",
		GoogleID:     &googleID,
		LastLoginAt:  &lastLogin,
	}
}

// The settings screen decides between "change your password" and "set a password" from this one
// field, because the domain accepts an empty current password exactly when there is no hash to
// verify against. Getting it backwards for a Google-only account asks them for a credential they
// have never had, which is a dead end with no error message that would explain it.
func TestHasPasswordDistinguishesAGoogleOnlyAccount(t *testing.T) {
	withPassword := FromDomain(sampleUser())
	if !withPassword.HasPassword {
		t.Error("has_password = false for a user with a password hash")
	}

	googleOnly := sampleUser()
	googleOnly.PasswordHash = ""
	wire := FromDomain(googleOnly)
	if wire.HasPassword {
		t.Error("has_password = true for a Google-only account with no hash")
	}
	// Credentials of some kind still exist, so has_account cannot stand in for this.
	if !wire.HasAccount {
		t.Error("has_account = false for a Google-only account; the two fields are not interchangeable")
	}
}

// This guard used to read "the payload contains no substring 'password'", which was true until
// has_password was added and is the cheapest possible way to state it. Adding a field is not a
// reason to weaken a guard, so the rule is restated precisely instead: the only key that may mention
// a password is the boolean saying whether one exists. Anything else — password_hash,
// password_changed_at, password_strength — fails here.
func TestNoPasswordFieldOtherThanTheBooleanReachesTheWire(t *testing.T) {
	payload, err := json.Marshal(FromDomain(sampleUser()))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var keyed map[string]any
	if err := json.Unmarshal(payload, &keyed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for key := range keyed {
		if strings.Contains(key, "password") && key != "has_password" {
			t.Errorf("payload carries a password field %q: %s", key, payload)
		}
	}
}

// HasPassword says a hash exists and nothing else. The risk in adding it is that the next person
// reaches for the hash itself — for a strength badge, a "last changed" readout — so the payload is
// asserted rather than the field list.
func TestUserPayloadCarriesNoCredentialMaterial(t *testing.T) {
	payload, err := json.Marshal(FromDomain(sampleUser()))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	body := string(payload)
	for _, forbidden := range []string{"argon2id", "$", "password_hash", "google", "last_login", "65536"} {
		if strings.Contains(strings.ToLower(body), forbidden) {
			t.Errorf("user payload contains %q: %s", forbidden, body)
		}
	}
	if !strings.Contains(body, `"has_password":true`) {
		t.Errorf("has_password is missing from the payload: %s", body)
	}
}
