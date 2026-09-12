package account

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jblabs/blindpulse-be/pkg/apperror"
	domainaccount "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/account"
	"github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/event"
	"github.com/shopspring/decimal"
)

// Platform defaults, applied when the caller does not override a gate. They match the PRD's
// stated limits: 5% max daily drawdown and a 1:2 minimum risk-to-reward.
var (
	defaultRiskPerTradePct     = decimal.RequireFromString("1.0")
	defaultMaxDailyDrawdownPct = decimal.RequireFromString("5.0")
	defaultMinRiskReward       = decimal.RequireFromString("2.0")
	defaultLeverage            = decimal.RequireFromString("1")
)

const defaultMaxOpenPositions = 5

func NewService(deps Dependencies) Service {
	if deps.Clock == nil {
		deps.Clock = func() time.Time { return time.Now().UTC() }
	}
	if deps.Topic == nil {
		deps.Topic = func(name string) string { return name }
	}
	return &service{deps: deps}
}

func (s *service) Open(ctx context.Context, userID uuid.UUID, in OpenInput) (*domainaccount.Account, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return nil, apperror.WithFields("VALIDATION_FAILED", []apperror.FieldError{{Field: "name", Rule: "required", Message: "name is required"}})
	}
	if in.InitialBalance.LessThanOrEqual(decimal.Zero) {
		return nil, apperror.WithFields("VALIDATION_FAILED", []apperror.FieldError{{Field: "initial_balance", Rule: "gt", Message: "initial_balance must be greater than zero"}})
	}
	taken, err := s.deps.Repo.ExistsByName(ctx, userID, name)
	if err != nil {
		return nil, err
	}
	if taken {
		return nil, apperror.New("ACCOUNT_NAME_TAKEN")
	}
	currency := strings.ToUpper(strings.TrimSpace(in.Currency))
	if currency == "" {
		currency = "USD"
	}
	now := s.deps.Clock()
	id := uuid.New()
	entity := &domainaccount.Account{
		ID: id, UserID: userID,
		// The first iteration is its own root: the tree is identified by the account that started
		// it, so every later iteration can point back at one stable ID.
		RootAccountID:  id,
		IterationIndex: 1,
		Name:           name, StrategyProfile: trimmedOrNil(in.StrategyProfile), Currency: currency,
		InitialBalance: in.InitialBalance, CurrentBalance: in.InitialBalance,
		CurrentEquity: in.InitialBalance, PeakEquity: in.InitialBalance,
		Risk:      resolveRisk(in.Risk, defaultRisk()),
		Status:    domainaccount.StatusActive,
		CreatedAt: now, UpdatedAt: now, Version: 1,
	}
	var created *domainaccount.Account
	err = s.deps.UOW.Do(ctx, func(ctx context.Context) error {
		var err error
		if created, err = s.deps.Repo.Create(ctx, entity); err != nil {
			return err
		}
		if _, err = s.appendLedger(ctx, created.ID, domainaccount.LedgerOpen, decimal.Zero, created.InitialBalance,
			created.InitialBalance, map[string]any{"reason": "account opened"}, nil, nil); err != nil {
			return err
		}
		return s.emit(ctx, "account", created.ID, event.TypeAccountOpened, event.TopicAccounts, event.AccountOpened{
			AccountID: created.ID, RootAccountID: created.RootAccountID, UserID: userID,
			IterationIndex: created.IterationIndex, Name: created.Name,
			StrategyProfile: derefOrEmpty(created.StrategyProfile), Currency: created.Currency,
			InitialBalance: created.InitialBalance, OpenedAt: now,
		})
	})
	if err != nil {
		return nil, err
	}
	return created, nil
}

// Reset is the non-destructive fork. It seals the live iteration - writing a terminal ledger entry
// and freezing its root hash - and opens the next one beside it. The sealed rows are never
// rewritten, so "Iteration 01: Initial Test / RESET DRAWDOWN" stays queryable forever.
func (s *service) Reset(ctx context.Context, userID, accountID uuid.UUID, in ResetInput) (*domainaccount.Account, error) {
	current, err := s.load(ctx, userID, accountID)
	if err != nil {
		return nil, err
	}
	if !current.IsActive() {
		return nil, apperror.New("ACCOUNT_ARCHIVED")
	}
	reason := strings.TrimSpace(in.Reason)
	if reason == "" {
		reason = "manual reset"
	}
	balance := current.InitialBalance
	if in.InitialBalance != nil {
		if in.InitialBalance.LessThanOrEqual(decimal.Zero) {
			return nil, apperror.WithFields("VALIDATION_FAILED", []apperror.FieldError{{Field: "initial_balance", Rule: "gt", Message: "initial_balance must be greater than zero"}})
		}
		balance = *in.InitialBalance
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		name = fmt.Sprintf("%s — Iteration %02d", rootName(current.Name), current.IterationIndex+1)
	}
	now := s.deps.Clock()
	child := &domainaccount.Account{
		ID: uuid.New(), UserID: userID,
		RootAccountID: current.RootAccountID, ParentAccountID: &current.ID,
		IterationIndex: current.IterationIndex + 1,
		Name:           name, StrategyProfile: trimmedOrNil(in.StrategyProfile), Currency: current.Currency,
		InitialBalance: balance, CurrentBalance: balance, CurrentEquity: balance, PeakEquity: balance,
		Risk:      resolveRisk(in.Risk, current.Risk),
		Status:    domainaccount.StatusActive,
		CreatedAt: now, UpdatedAt: now, Version: 1,
	}
	if child.StrategyProfile == nil {
		child.StrategyProfile = current.StrategyProfile
	}
	var created *domainaccount.Account
	err = s.deps.UOW.Do(ctx, func(ctx context.Context) error {
		sealEntry, err := s.appendLedger(ctx, current.ID, domainaccount.LedgerSeal, decimal.Zero,
			current.CurrentBalance, current.CurrentEquity, map[string]any{"reason": reason}, nil, nil)
		if err != nil {
			return err
		}
		// The seal entry's hash becomes the iteration's root hash: it covers the whole chain
		// beneath it, so one string is enough to attest the entire history.
		if err := s.deps.Repo.Seal(ctx, current.ID, reason, sealEntry.EntryHash, current.Version); err != nil {
			return err
		}
		if created, err = s.deps.Repo.Create(ctx, child); err != nil {
			return err
		}
		if _, err = s.appendLedger(ctx, created.ID, domainaccount.LedgerOpen, decimal.Zero, created.InitialBalance,
			created.InitialBalance, map[string]any{"reason": reason, "forked_from": current.ID.String()}, nil, nil); err != nil {
			return err
		}
		return s.emit(ctx, "account", created.RootAccountID, event.TypeAccountReset, event.TopicAccounts, event.AccountReset{
			RootAccountID: current.RootAccountID, SealedAccountID: current.ID, NewAccountID: created.ID,
			UserID: userID, IterationIndex: created.IterationIndex, Reason: reason,
			FinalBalance: current.CurrentBalance, FinalEquity: current.CurrentEquity,
			SealedRootHash: sealEntry.EntryHash, ResetAt: now,
		})
	})
	if err != nil {
		return nil, err
	}
	return created, nil
}

func (s *service) Get(ctx context.Context, userID, accountID uuid.UUID) (*domainaccount.Account, error) {
	return s.load(ctx, userID, accountID)
}

func (s *service) ListTrees(ctx context.Context, userID uuid.UUID) ([]domainaccount.Tree, error) {
	accounts, err := s.deps.Repo.ListByUserID(ctx, userID)
	if err != nil {
		return nil, err
	}
	order := make([]uuid.UUID, 0)
	grouped := make(map[uuid.UUID][]domainaccount.Account)
	for _, entity := range accounts {
		if _, seen := grouped[entity.RootAccountID]; !seen {
			order = append(order, entity.RootAccountID)
		}
		grouped[entity.RootAccountID] = append(grouped[entity.RootAccountID], entity)
	}
	trees := make([]domainaccount.Tree, 0, len(order))
	for _, root := range order {
		trees = append(trees, domainaccount.Tree{RootAccountID: root, Iterations: grouped[root]})
	}
	return trees, nil
}

func (s *service) GetTree(ctx context.Context, userID, rootAccountID uuid.UUID) (*domainaccount.Tree, error) {
	if _, err := s.load(ctx, userID, rootAccountID); err != nil {
		return nil, err
	}
	iterations, err := s.deps.Repo.ListByRootID(ctx, rootAccountID)
	if err != nil {
		return nil, err
	}
	if len(iterations) == 0 {
		return nil, apperror.New("ACCOUNT_NOT_FOUND")
	}
	return &domainaccount.Tree{RootAccountID: rootAccountID, Iterations: iterations}, nil
}

func (s *service) Ledger(ctx context.Context, userID, accountID uuid.UUID) ([]domainaccount.LedgerEntry, error) {
	if _, err := s.load(ctx, userID, accountID); err != nil {
		return nil, err
	}
	return s.deps.Ledger.ListByAccountID(ctx, accountID)
}

func (s *service) VerifyLedger(ctx context.Context, userID, accountID uuid.UUID) (*Verification, error) {
	entity, err := s.load(ctx, userID, accountID)
	if err != nil {
		return nil, err
	}
	entries, err := s.deps.Ledger.ListByAccountID(ctx, accountID)
	if err != nil {
		return nil, err
	}
	result := &Verification{AccountID: accountID.String(), Entries: len(entries), Valid: true}
	if entity.RootHash != nil {
		result.StoredRootHash = *entity.RootHash
	}
	var previous *string
	for _, entry := range entries {
		expected := HashLedgerEntry(entry, previous)
		if expected != entry.EntryHash {
			sequence := entry.Sequence
			result.Valid = false
			result.BrokenAtSequence = &sequence
			return result, nil
		}
		hash := entry.EntryHash
		previous = &hash
	}
	if previous != nil {
		result.RootHash = *previous
	}
	// A sealed account publishes its root hash; if the recomputed chain no longer ends at that
	// value, the ledger has been altered since sealing even though each link still verifies.
	if result.StoredRootHash != "" && result.StoredRootHash != result.RootHash {
		result.Valid = false
	}
	return result, nil
}

func (s *service) load(ctx context.Context, userID, accountID uuid.UUID) (*domainaccount.Account, error) {
	entity, err := s.deps.Repo.GetByID(ctx, accountID)
	if err != nil {
		return nil, err
	}
	if entity.UserID != userID {
		// Reported as not-found rather than forbidden: confirming that an account ID exists is
		// itself information about another trader's portfolio.
		return nil, apperror.New("ACCOUNT_NOT_FOUND")
	}
	return entity, nil
}

func (s *service) appendLedger(ctx context.Context, accountID uuid.UUID, kind domainaccount.LedgerKind,
	amount, balanceAfter, equityAfter decimal.Decimal, payload map[string]any,
	referenceType *string, referenceID *uuid.UUID) (*domainaccount.LedgerEntry, error) {
	last, err := s.deps.Ledger.Last(ctx, accountID)
	if err != nil && !apperror.Is(err, "ACCOUNT_NOT_FOUND") {
		return nil, err
	}
	var previousHash *string
	sequence := int64(1)
	if last != nil {
		hash := last.EntryHash
		previousHash = &hash
		sequence = last.Sequence + 1
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, apperror.Wrap(err, "INTERNAL_ERROR")
	}
	entry := &domainaccount.LedgerEntry{
		ID: uuid.New(), AccountID: accountID, Sequence: sequence, Kind: kind,
		ReferenceType: referenceType, ReferenceID: referenceID,
		Amount: amount, BalanceAfter: balanceAfter, EquityAfter: equityAfter,
		Payload: encoded, PreviousHash: previousHash,
		// Truncated to the resolution PostgreSQL actually stores. A nanosecond in the hash is a
		// nanosecond the database rounds away, and the chain then fails to verify on read-back.
		RecordedAt: s.deps.Clock().Truncate(time.Microsecond),
	}
	entry.EntryHash = HashLedgerEntry(*entry, previousHash)
	if err := s.deps.Ledger.Append(ctx, entry); err != nil {
		return nil, err
	}
	return entry, nil
}

// HashLedgerEntry is the chain link. Fields are joined with a separator that cannot appear in any of
// them, so two different entries cannot serialize to the same string by shifting a boundary.
//
// Exported because the execution domain appends trade entries to the same chain. There can only be
// one implementation of this rule: a second copy that drifted by a field would make VerifyLedger
// fail for every account that had ever traded, and the failure would look like tampering.
func HashLedgerEntry(entry domainaccount.LedgerEntry, previousHash *string) string {
	previous := ""
	if previousHash != nil {
		previous = *previousHash
	}
	digest := sha256.Sum256([]byte(strings.Join([]string{
		previous,
		entry.AccountID.String(),
		fmt.Sprintf("%d", entry.Sequence),
		string(entry.Kind),
		entry.Amount.String(),
		entry.BalanceAfter.String(),
		entry.EquityAfter.String(),
		string(entry.Payload),
		entry.RecordedAt.UTC().Format(time.RFC3339Nano),
	}, "\x1f")))
	return hex.EncodeToString(digest[:])
}

func (s *service) emit(ctx context.Context, aggregateType string, aggregateID uuid.UUID, eventType, topic string, payload any) error {
	if s.deps.Outbox == nil {
		return nil
	}
	entity, err := event.New(aggregateType, aggregateID, eventType, s.deps.Topic(topic), payload)
	if err != nil {
		return apperror.Wrap(err, "INTERNAL_ERROR")
	}
	return s.deps.Outbox.Create(ctx, entity)
}

func defaultRisk() domainaccount.RiskPolicy {
	return domainaccount.RiskPolicy{
		RiskPerTradePct: defaultRiskPerTradePct, MaxDailyDrawdownPct: defaultMaxDailyDrawdownPct,
		MinRiskReward: defaultMinRiskReward, MaxOpenPositions: defaultMaxOpenPositions, Leverage: defaultLeverage,
	}
}

func resolveRisk(in RiskInput, base domainaccount.RiskPolicy) domainaccount.RiskPolicy {
	policy := base
	if in.RiskPerTradePct != nil {
		policy.RiskPerTradePct = *in.RiskPerTradePct
	}
	if in.MaxDailyDrawdownPct != nil {
		policy.MaxDailyDrawdownPct = *in.MaxDailyDrawdownPct
	}
	if in.MinRiskReward != nil {
		policy.MinRiskReward = *in.MinRiskReward
	}
	if in.MaxOpenPositions != nil {
		policy.MaxOpenPositions = *in.MaxOpenPositions
	}
	if in.Leverage != nil {
		policy.Leverage = *in.Leverage
	}
	return policy
}

// rootName strips a previously appended iteration suffix so repeated resets read
// "Swing Replay — Iteration 04" rather than accumulating one suffix per reset.
func rootName(name string) string {
	if index := strings.Index(name, " — Iteration "); index > 0 {
		return name[:index]
	}
	return name
}

func trimmedOrNil(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func derefOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
