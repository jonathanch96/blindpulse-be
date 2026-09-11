package user

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jblabs/blindpulse-be/pkg/apperror"
	domainuser "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/user"
)

// The two paths the account settings screen rides on: editing a profile, and changing or setting a
// password. Both were reachable from the API before the screen existed and neither was covered.

type userRepoStub struct {
	rows map[uuid.UUID]*domainuser.User
}

func newUserRepoStub() *userRepoStub {
	return &userRepoStub{rows: make(map[uuid.UUID]*domainuser.User)}
}

func (r *userRepoStub) put(entity domainuser.User) *domainuser.User {
	stored := entity
	r.rows[entity.ID] = &stored
	return &stored
}

func (r *userRepoStub) GetByID(_ context.Context, id uuid.UUID) (*domainuser.User, error) {
	entity, ok := r.rows[id]
	if !ok {
		return nil, apperror.New("USER_NOT_FOUND")
	}
	// A copy, so a service that mutates what it read cannot change stored state without an Update.
	copied := *entity
	return &copied, nil
}

func (r *userRepoStub) Update(_ context.Context, entity *domainuser.User) (*domainuser.User, error) {
	stored := *entity
	r.rows[entity.ID] = &stored
	updated := stored
	return &updated, nil
}

func (r *userRepoStub) SetPasswordHash(_ context.Context, id uuid.UUID, hash string) error {
	entity, ok := r.rows[id]
	if !ok {
		return apperror.New("USER_NOT_FOUND")
	}
	entity.PasswordHash = hash
	return nil
}

func (r *userRepoStub) Create(context.Context, *domainuser.User) (*domainuser.User, error) {
	return nil, apperror.New("NOT_IMPLEMENTED")
}
func (r *userRepoStub) GetByEmail(context.Context, string) (*domainuser.User, error) {
	return nil, apperror.New("USER_NOT_FOUND")
}
func (r *userRepoStub) GetByGoogleID(context.Context, string) (*domainuser.User, error) {
	return nil, apperror.New("USER_NOT_FOUND")
}
func (r *userRepoStub) ExistsByEmail(context.Context, string) (bool, error) { return false, nil }
func (r *userRepoStub) SetGoogleID(context.Context, uuid.UUID, string) error {
	return apperror.New("NOT_IMPLEMENTED")
}
func (r *userRepoStub) TouchLastLogin(context.Context, uuid.UUID, time.Time) error { return nil }
func (r *userRepoStub) ListByIDs(context.Context, []uuid.UUID) ([]domainuser.User, error) {
	return nil, nil
}

// reversibleHasher is not a password hash and is not pretending to be one. It is a marker the test
// can read back, so an assertion can say "the stored hash is of this password" without depending on
// Argon2id's parameters or its cost.
type reversibleHasher struct{}

func (reversibleHasher) Hash(value string) (string, error) { return "hashed:" + value, nil }
func (reversibleHasher) Verify(value, hash string) (bool, error) {
	return hash == "hashed:"+value, nil
}

func serviceWith(repo Repository) Service {
	return NewService(Dependencies{Repo: repo, Hasher: reversibleHasher{}})
}

func TestUpdateProfileTrimsTheNameAndLeavesOmittedFieldsAlone(t *testing.T) {
	repo := newUserRepoStub()
	avatar := "https://cdn.example.com/a.png"
	id := uuid.New()
	repo.put(domainuser.User{ID: id, Email: "trader@example.com", Name: "Trader", AvatarURL: &avatar})

	name := "  Renamed Trader  "
	updated, err := serviceWith(repo).UpdateProfile(context.Background(), id, UpdateProfileInput{Name: &name})
	if err != nil {
		t.Fatalf("UpdateProfile: %v", err)
	}
	if updated.Name != "Renamed Trader" {
		t.Errorf("name = %q, want the trimmed form", updated.Name)
	}
	// The form submits only what it carries. An avatar dropped because the caller didn't mention it
	// would be a silent loss, so a nil field has to mean "unchanged" rather than "clear".
	if updated.AvatarURL == nil || *updated.AvatarURL != avatar {
		t.Errorf("avatar = %v, want it untouched at %q", updated.AvatarURL, avatar)
	}
}

func TestUpdateProfileClearsTheAvatarOnAnExplicitBlank(t *testing.T) {
	repo := newUserRepoStub()
	avatar := "https://cdn.example.com/a.png"
	id := uuid.New()
	repo.put(domainuser.User{ID: id, Email: "trader@example.com", Name: "Trader", AvatarURL: &avatar})

	blank := "   "
	updated, err := serviceWith(repo).UpdateProfile(context.Background(), id, UpdateProfileInput{AvatarURL: &blank})
	if err != nil {
		t.Fatalf("UpdateProfile: %v", err)
	}
	// NULL rather than "": an empty avatar_url reaches the UI as <img src="">, which re-requests the
	// current page. The distinction is also the only way "remove my avatar" is expressible at all.
	if updated.AvatarURL != nil {
		t.Errorf("avatar = %q, want nil after an explicit blank", *updated.AvatarURL)
	}
}

func TestChangePasswordSetsTheFirstPasswordForAGoogleOnlyAccount(t *testing.T) {
	repo := newUserRepoStub()
	googleID := "google-subject-117"
	id := uuid.New()
	repo.put(domainuser.User{ID: id, Email: "trader@example.com", Name: "Trader", GoogleID: &googleID})

	// No current password, because there is no password. This is the branch the settings screen
	// reads has_password to find: asking a Google-only account for a credential it never had is a
	// dead end the UI cannot explain.
	err := serviceWith(repo).ChangePassword(context.Background(), id, ChangePasswordInput{NewPassword: "Str0ng!Passw0rd"})
	if err != nil {
		t.Fatalf("ChangePassword on a Google-only account: %v", err)
	}
	if got := repo.rows[id].PasswordHash; got != "hashed:Str0ng!Passw0rd" {
		t.Errorf("stored hash = %q, want the new password hashed", got)
	}
}

func TestChangePasswordRefusesAWrongCurrentPasswordAndKeepsTheOldOne(t *testing.T) {
	repo := newUserRepoStub()
	id := uuid.New()
	repo.put(domainuser.User{ID: id, Email: "trader@example.com", Name: "Trader", PasswordHash: "hashed:Original!1"})

	err := serviceWith(repo).ChangePassword(context.Background(), id, ChangePasswordInput{
		CurrentPassword: "NotTheOne!1", NewPassword: "Str0ng!Passw0rd",
	})
	if !apperror.Is(err, "INVALID_CURRENT_PASSWORD") {
		t.Fatalf("err = %v, want INVALID_CURRENT_PASSWORD", err)
	}
	// The specific failure this guards: an empty current password must not be treated as "no
	// password set" for an account that has one, which would let anyone with the session change it.
	if got := repo.rows[id].PasswordHash; got != "hashed:Original!1" {
		t.Errorf("stored hash = %q, want the original left in place", got)
	}
}

func TestChangePasswordRejectsANewPasswordThatBreaksThePolicy(t *testing.T) {
	repo := newUserRepoStub()
	id := uuid.New()
	repo.put(domainuser.User{ID: id, Email: "trader@example.com", Name: "Trader", PasswordHash: "hashed:Original!1"})

	err := serviceWith(repo).ChangePassword(context.Background(), id, ChangePasswordInput{
		CurrentPassword: "Original!1", NewPassword: "short",
	})
	if !apperror.Is(err, "VALIDATION_FAILED") {
		t.Fatalf("err = %v, want VALIDATION_FAILED", err)
	}
	if got := repo.rows[id].PasswordHash; !strings.HasPrefix(got, "hashed:Original") {
		t.Errorf("stored hash = %q, want the original left in place", got)
	}
}
