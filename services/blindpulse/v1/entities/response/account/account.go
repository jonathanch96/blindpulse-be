package accountresponse

import (
	"time"

	domainaccount "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/account"
)

type Risk struct {
	RiskPerTradePct     string `json:"risk_per_trade_pct"`
	MaxDailyDrawdownPct string `json:"max_daily_drawdown_pct"`
	MinRiskReward       string `json:"min_risk_reward"`
	MaxOpenPositions    int    `json:"max_open_positions"`
	Leverage            string `json:"leverage"`
}

type Account struct {
	ID              string     `json:"id"`
	RootAccountID   string     `json:"root_account_id"`
	ParentAccountID *string    `json:"parent_account_id"`
	IterationIndex  int        `json:"iteration_index"`
	Name            string     `json:"name"`
	StrategyProfile *string    `json:"strategy_profile"`
	Currency        string     `json:"currency"`
	InitialBalance  string     `json:"initial_balance"`
	CurrentBalance  string     `json:"current_balance"`
	CurrentEquity   string     `json:"current_equity"`
	PeakEquity      string     `json:"peak_equity"`
	NetReturnPct    string     `json:"net_return_pct"`
	DrawdownPct     string     `json:"drawdown_pct"`
	Risk            Risk       `json:"risk"`
	Status          string     `json:"status"`
	ResetReason     *string    `json:"reset_reason"`
	ResetAt         *time.Time `json:"reset_at"`
	SealedAt        *time.Time `json:"sealed_at"`
	RootHash        *string    `json:"root_hash"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

type Tree struct {
	RootAccountID   string    `json:"root_account_id"`
	ActiveAccountID *string   `json:"active_account_id"`
	Iterations      []Account `json:"iterations"`
}

type LedgerEntry struct {
	ID            string    `json:"id"`
	Sequence      int64     `json:"sequence"`
	Kind          string    `json:"kind"`
	ReferenceType *string   `json:"reference_type"`
	ReferenceID   *string   `json:"reference_id"`
	Amount        string    `json:"amount"`
	BalanceAfter  string    `json:"balance_after"`
	EquityAfter   string    `json:"equity_after"`
	PreviousHash  *string   `json:"previous_hash"`
	EntryHash     string    `json:"entry_hash"`
	RecordedAt    time.Time `json:"recorded_at"`
}

type Verification struct {
	AccountID        string `json:"account_id"`
	Entries          int    `json:"entries"`
	RootHash         string `json:"root_hash"`
	StoredRootHash   string `json:"stored_root_hash"`
	Valid            bool   `json:"valid"`
	BrokenAtSequence *int64 `json:"broken_at_sequence"`
}

func FromDomain(entity domainaccount.Account) Account {
	var parent *string
	if entity.ParentAccountID != nil {
		value := entity.ParentAccountID.String()
		parent = &value
	}
	return Account{
		ID: entity.ID.String(), RootAccountID: entity.RootAccountID.String(), ParentAccountID: parent,
		IterationIndex: entity.IterationIndex, Name: entity.Name, StrategyProfile: entity.StrategyProfile,
		Currency: entity.Currency, InitialBalance: entity.InitialBalance.String(),
		CurrentBalance: entity.CurrentBalance.String(), CurrentEquity: entity.CurrentEquity.String(),
		PeakEquity:   entity.PeakEquity.String(),
		NetReturnPct: entity.NetReturnPct().StringFixed(2), DrawdownPct: entity.DrawdownPct().StringFixed(2),
		Risk: Risk{
			RiskPerTradePct: entity.Risk.RiskPerTradePct.String(), MaxDailyDrawdownPct: entity.Risk.MaxDailyDrawdownPct.String(),
			MinRiskReward: entity.Risk.MinRiskReward.String(), MaxOpenPositions: entity.Risk.MaxOpenPositions,
			Leverage: entity.Risk.Leverage.String(),
		},
		Status: string(entity.Status), ResetReason: entity.ResetReason, ResetAt: entity.ResetAt,
		SealedAt: entity.SealedAt, RootHash: entity.RootHash,
		CreatedAt: entity.CreatedAt, UpdatedAt: entity.UpdatedAt,
	}
}

func FromDomains(entities []domainaccount.Account) []Account {
	accounts := make([]Account, 0, len(entities))
	for _, entity := range entities {
		accounts = append(accounts, FromDomain(entity))
	}
	return accounts
}

func FromTree(tree domainaccount.Tree) Tree {
	var active *string
	if entity := tree.Active(); entity != nil {
		value := entity.ID.String()
		active = &value
	}
	return Tree{RootAccountID: tree.RootAccountID.String(), ActiveAccountID: active, Iterations: FromDomains(tree.Iterations)}
}

func FromTrees(trees []domainaccount.Tree) []Tree {
	result := make([]Tree, 0, len(trees))
	for _, tree := range trees {
		result = append(result, FromTree(tree))
	}
	return result
}

func FromLedgerEntry(entry domainaccount.LedgerEntry) LedgerEntry {
	var reference *string
	if entry.ReferenceID != nil {
		value := entry.ReferenceID.String()
		reference = &value
	}
	return LedgerEntry{
		ID: entry.ID.String(), Sequence: entry.Sequence, Kind: string(entry.Kind),
		ReferenceType: entry.ReferenceType, ReferenceID: reference, Amount: entry.Amount.String(),
		BalanceAfter: entry.BalanceAfter.String(), EquityAfter: entry.EquityAfter.String(),
		PreviousHash: entry.PreviousHash, EntryHash: entry.EntryHash, RecordedAt: entry.RecordedAt,
	}
}

func FromLedgerEntries(entries []domainaccount.LedgerEntry) []LedgerEntry {
	result := make([]LedgerEntry, 0, len(entries))
	for _, entry := range entries {
		result = append(result, FromLedgerEntry(entry))
	}
	return result
}
