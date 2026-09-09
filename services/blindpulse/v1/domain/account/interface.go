package account

import (
	"context"

	"github.com/google/uuid"
	domainaccount "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/account"
	"github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/event"
)

type Service interface {
	// Open starts a new reset tree: iteration 1, its own root, with an opening ledger entry.
	Open(ctx context.Context, userID uuid.UUID, in OpenInput) (*domainaccount.Account, error)
	// Reset seals the tree's active iteration and opens the next one. It never edits or deletes
	// the sealed iteration - that is the whole point of the feature.
	Reset(ctx context.Context, userID, accountID uuid.UUID, in ResetInput) (*domainaccount.Account, error)
	Get(ctx context.Context, userID, accountID uuid.UUID) (*domainaccount.Account, error)
	// ListTrees returns every root the user owns with all of its iterations attached.
	ListTrees(ctx context.Context, userID uuid.UUID) ([]domainaccount.Tree, error)
	GetTree(ctx context.Context, userID, rootAccountID uuid.UUID) (*domainaccount.Tree, error)
	// Ledger returns the account's append-only entries, oldest first.
	Ledger(ctx context.Context, userID, accountID uuid.UUID) ([]domainaccount.LedgerEntry, error)
	// VerifyLedger recomputes the hash chain and reports whether the stored RootHash still holds.
	// It is what backs the "CRYPTOGRAPHIC INTEGRITY / VERIFIED" badge, which would be a decoration
	// rather than a claim if nothing ever recomputed it.
	VerifyLedger(ctx context.Context, userID, accountID uuid.UUID) (*Verification, error)
}

type Repository interface {
	Create(context.Context, *domainaccount.Account) (*domainaccount.Account, error)
	GetByID(context.Context, uuid.UUID) (*domainaccount.Account, error)
	ListByUserID(context.Context, uuid.UUID) ([]domainaccount.Account, error)
	ListByRootID(context.Context, uuid.UUID) ([]domainaccount.Account, error)
	GetActiveByRootID(context.Context, uuid.UUID) (*domainaccount.Account, error)
	ExistsByName(ctx context.Context, userID uuid.UUID, name string) (bool, error)
	// Seal closes an iteration for good: status, sealed_at, and the ledger root hash, in one
	// optimistically-locked update.
	Seal(ctx context.Context, id uuid.UUID, reason, rootHash string, version int) error
	UpdateBalances(context.Context, *domainaccount.Account) error
}

type LedgerRepository interface {
	Append(context.Context, *domainaccount.LedgerEntry) error
	ListByAccountID(context.Context, uuid.UUID) ([]domainaccount.LedgerEntry, error)
	Last(context.Context, uuid.UUID) (*domainaccount.LedgerEntry, error)
}

// OutboxRepository is the domain's view of the transactional outbox. The domain never talks to
// Kafka: it records the fact, and the relay decides when it reaches a broker.
type OutboxRepository interface {
	Create(context.Context, *event.OutboxEvent) error
}

// UnitOfWork runs a closure inside one database transaction. A reset writes to two accounts and
// two ledgers; either all four land or none do, or the tree gains a second live iteration.
type UnitOfWork interface {
	Do(ctx context.Context, fn func(ctx context.Context) error) error
}
