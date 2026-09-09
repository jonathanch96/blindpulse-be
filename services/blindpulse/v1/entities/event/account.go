package event

import (
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

type AccountOpened struct {
	AccountID       uuid.UUID       `json:"account_id"`
	RootAccountID   uuid.UUID       `json:"root_account_id"`
	ParentAccountID *uuid.UUID      `json:"parent_account_id,omitempty"`
	UserID          uuid.UUID       `json:"user_id"`
	IterationIndex  int             `json:"iteration_index"`
	Name            string          `json:"name"`
	StrategyProfile string          `json:"strategy_profile,omitempty"`
	Currency        string          `json:"currency"`
	InitialBalance  decimal.Decimal `json:"initial_balance"`
	OpenedAt        time.Time       `json:"opened_at"`
}

// AccountReset carries both sides of the fork: the iteration being sealed and the one taking over.
// A consumer that only saw the new account could not tell a reset apart from a fresh sign-up.
type AccountReset struct {
	RootAccountID   uuid.UUID       `json:"root_account_id"`
	SealedAccountID uuid.UUID       `json:"sealed_account_id"`
	NewAccountID    uuid.UUID       `json:"new_account_id"`
	UserID          uuid.UUID       `json:"user_id"`
	IterationIndex  int             `json:"iteration_index"`
	Reason          string          `json:"reason"`
	FinalBalance    decimal.Decimal `json:"final_balance"`
	FinalEquity     decimal.Decimal `json:"final_equity"`
	SealedRootHash  string          `json:"sealed_root_hash"`
	ResetAt         time.Time       `json:"reset_at"`
}
