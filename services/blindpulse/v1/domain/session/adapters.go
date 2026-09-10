package session

import (
	"context"

	"github.com/google/uuid"
	"github.com/jblabs/blindpulse-be/pkg/apperror"
	domainaccount "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/account"
)

// AccountLookup is the narrow slice of the account repository this domain needs. Declaring it here
// rather than importing the account domain keeps the two aggregates independent: a session cares
// only that the account exists, belongs to the caller, and is still live.
type AccountLookup interface {
	GetByID(context.Context, uuid.UUID) (*domainaccount.Account, error)
}

type accountReader struct{ accounts AccountLookup }

// NewAccountReader adapts an account repository to the session domain's AccountReader.
func NewAccountReader(accounts AccountLookup) AccountReader { return accountReader{accounts: accounts} }

func (r accountReader) OwnedActiveAccount(ctx context.Context, userID, accountID uuid.UUID) error {
	entity, err := r.accounts.GetByID(ctx, accountID)
	if err != nil {
		return err
	}
	if entity.UserID != userID {
		// Not-found rather than forbidden: confirming the id exists leaks somebody else's account.
		return apperror.New("ACCOUNT_NOT_FOUND")
	}
	if !entity.IsActive() {
		// A sealed iteration is immutable history. Trading into it would rewrite a record the
		// ledger has already attested.
		return apperror.New("ACCOUNT_ARCHIVED")
	}
	return nil
}
