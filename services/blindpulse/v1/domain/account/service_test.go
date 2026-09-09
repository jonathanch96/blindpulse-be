package account

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jblabs/blindpulse-be/pkg/apperror"
	domainaccount "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/account"
	"github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/event"
	"github.com/shopspring/decimal"
)

type accountRepoStub struct {
	rows map[uuid.UUID]*domainaccount.Account
}

func newAccountRepoStub() *accountRepoStub {
	return &accountRepoStub{rows: make(map[uuid.UUID]*domainaccount.Account)}
}

func (r *accountRepoStub) Create(_ context.Context, entity *domainaccount.Account) (*domainaccount.Account, error) {
	stored := *entity
	r.rows[entity.ID] = &stored
	created := stored
	return &created, nil
}

func (r *accountRepoStub) GetByID(_ context.Context, id uuid.UUID) (*domainaccount.Account, error) {
	entity, ok := r.rows[id]
	if !ok {
		return nil, apperror.New("ACCOUNT_NOT_FOUND")
	}
	copied := *entity
	return &copied, nil
}

func (r *accountRepoStub) ListByUserID(_ context.Context, userID uuid.UUID) ([]domainaccount.Account, error) {
	return r.filter(func(entity domainaccount.Account) bool { return entity.UserID == userID }), nil
}

func (r *accountRepoStub) ListByRootID(_ context.Context, rootID uuid.UUID) ([]domainaccount.Account, error) {
	return r.filter(func(entity domainaccount.Account) bool { return entity.RootAccountID == rootID }), nil
}

func (r *accountRepoStub) GetActiveByRootID(_ context.Context, rootID uuid.UUID) (*domainaccount.Account, error) {
	for _, entity := range r.rows {
		if entity.RootAccountID == rootID && entity.IsActive() {
			copied := *entity
			return &copied, nil
		}
	}
	return nil, apperror.New("ACCOUNT_NOT_FOUND")
}

func (r *accountRepoStub) ExistsByName(_ context.Context, userID uuid.UUID, name string) (bool, error) {
	for _, entity := range r.rows {
		if entity.UserID == userID && entity.Name == name {
			return true, nil
		}
	}
	return false, nil
}

func (r *accountRepoStub) Seal(_ context.Context, id uuid.UUID, reason, rootHash string, version int) error {
	entity, ok := r.rows[id]
	if !ok || entity.Version != version || !entity.IsActive() {
		return apperror.New("CONCURRENT_MODIFICATION")
	}
	now := time.Now().UTC()
	entity.Status = domainaccount.StatusReset
	entity.ResetReason = &reason
	entity.ResetAt = &now
	entity.SealedAt = &now
	entity.RootHash = &rootHash
	entity.Version++
	return nil
}

func (r *accountRepoStub) UpdateBalances(_ context.Context, entity *domainaccount.Account) error {
	stored, ok := r.rows[entity.ID]
	if !ok || stored.Version != entity.Version {
		return apperror.New("CONCURRENT_MODIFICATION")
	}
	stored.CurrentBalance = entity.CurrentBalance
	stored.CurrentEquity = entity.CurrentEquity
	stored.PeakEquity = entity.PeakEquity
	stored.Version++
	return nil
}

func (r *accountRepoStub) filter(keep func(domainaccount.Account) bool) []domainaccount.Account {
	rows := make([]domainaccount.Account, 0)
	for _, entity := range r.rows {
		if keep(*entity) {
			rows = append(rows, *entity)
		}
	}
	for i := 1; i < len(rows); i++ {
		for j := i; j > 0 && rows[j].IterationIndex < rows[j-1].IterationIndex; j-- {
			rows[j], rows[j-1] = rows[j-1], rows[j]
		}
	}
	return rows
}

type ledgerRepoStub struct {
	rows map[uuid.UUID][]domainaccount.LedgerEntry
}

func newLedgerRepoStub() *ledgerRepoStub {
	return &ledgerRepoStub{rows: make(map[uuid.UUID][]domainaccount.LedgerEntry)}
}

func (r *ledgerRepoStub) Append(_ context.Context, entry *domainaccount.LedgerEntry) error {
	r.rows[entry.AccountID] = append(r.rows[entry.AccountID], *entry)
	return nil
}

func (r *ledgerRepoStub) ListByAccountID(_ context.Context, accountID uuid.UUID) ([]domainaccount.LedgerEntry, error) {
	return append([]domainaccount.LedgerEntry(nil), r.rows[accountID]...), nil
}

func (r *ledgerRepoStub) Last(_ context.Context, accountID uuid.UUID) (*domainaccount.LedgerEntry, error) {
	entries := r.rows[accountID]
	if len(entries) == 0 {
		return nil, nil
	}
	last := entries[len(entries)-1]
	return &last, nil
}

type outboxStub struct{ events []event.OutboxEvent }

func (o *outboxStub) Create(_ context.Context, entity *event.OutboxEvent) error {
	o.events = append(o.events, *entity)
	return nil
}

type passthroughUOW struct{}

func (passthroughUOW) Do(ctx context.Context, fn func(context.Context) error) error { return fn(ctx) }

func newTestService() (Service, *accountRepoStub, *ledgerRepoStub, *outboxStub) {
	repo, ledger, outbox := newAccountRepoStub(), newLedgerRepoStub(), &outboxStub{}
	service := NewService(Dependencies{Repo: repo, Ledger: ledger, Outbox: outbox, UOW: passthroughUOW{}})
	return service, repo, ledger, outbox
}

func openTestAccount(t *testing.T, service Service, userID uuid.UUID) *domainaccount.Account {
	t.Helper()
	entity, err := service.Open(context.Background(), userID, OpenInput{
		Name: "Swing Replay", StrategyProfile: "Liquidity Sweep", Currency: "usd",
		InitialBalance: decimal.NewFromInt(10000),
	})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	return entity
}

func TestOpenStartsItsOwnTreeWithAnOpeningLedgerEntry(t *testing.T) {
	t.Parallel()
	service, _, ledger, outbox := newTestService()
	userID := uuid.New()

	entity := openTestAccount(t, service, userID)

	if entity.RootAccountID != entity.ID || entity.ParentAccountID != nil || entity.IterationIndex != 1 {
		t.Fatalf("first iteration is not its own root: %+v", entity)
	}
	if entity.Currency != "USD" {
		t.Fatalf("currency = %q, want normalized USD", entity.Currency)
	}
	// The defaults are the PRD's stated gates, and the account carries them rather than the
	// handler, so an order placed through any path is measured against the same numbers.
	if entity.Risk.MaxDailyDrawdownPct.String() != "5" || entity.Risk.MinRiskReward.String() != "2" {
		t.Fatalf("risk defaults = %+v", entity.Risk)
	}
	entries := ledger.rows[entity.ID]
	if len(entries) != 1 || entries[0].Kind != domainaccount.LedgerOpen || entries[0].PreviousHash != nil {
		t.Fatalf("opening ledger entry = %+v", entries)
	}
	if len(outbox.events) != 1 || outbox.events[0].EventType != event.TypeAccountOpened {
		t.Fatalf("outbox events = %+v", outbox.events)
	}
}

func TestResetSealsTheOldIterationAndKeepsIt(t *testing.T) {
	t.Parallel()
	service, repo, ledger, outbox := newTestService()
	userID := uuid.New()
	first := openTestAccount(t, service, userID)

	second, err := service.Reset(context.Background(), userID, first.ID, ResetInput{Reason: "max drawdown breached"})
	if err != nil {
		t.Fatalf("Reset() error = %v", err)
	}

	sealed, err := repo.GetByID(context.Background(), first.ID)
	if err != nil {
		t.Fatalf("sealed iteration was removed: %v", err)
	}
	// The point of the feature: the blown iteration still exists, still carries its history, and
	// now carries a root hash attesting that history.
	if sealed.Status != domainaccount.StatusReset || sealed.SealedAt == nil || sealed.RootHash == nil {
		t.Fatalf("previous iteration was not sealed: %+v", sealed)
	}
	if got := len(ledger.rows[first.ID]); got != 2 {
		t.Fatalf("sealed ledger entries = %d, want the opening entry plus the seal", got)
	}
	if second.IterationIndex != 2 || second.ParentAccountID == nil || *second.ParentAccountID != first.ID {
		t.Fatalf("child iteration = %+v", second)
	}
	if second.RootAccountID != first.RootAccountID {
		t.Fatalf("child left the tree: root = %s, want %s", second.RootAccountID, first.RootAccountID)
	}
	// Risk policy and strategy profile are inherited, so a reset re-runs the same experiment
	// rather than silently starting a different one.
	if second.Risk != first.Risk || second.StrategyProfile == nil || *second.StrategyProfile != "Liquidity Sweep" {
		t.Fatalf("child did not inherit the parent's configuration: %+v", second)
	}
	if len(outbox.events) != 2 || outbox.events[1].EventType != event.TypeAccountReset {
		t.Fatalf("outbox events = %+v", outbox.events)
	}
}

func TestResetRefusesASealedIteration(t *testing.T) {
	t.Parallel()
	service, _, _, _ := newTestService()
	userID := uuid.New()
	first := openTestAccount(t, service, userID)
	if _, err := service.Reset(context.Background(), userID, first.ID, ResetInput{Reason: "first"}); err != nil {
		t.Fatalf("first Reset() error = %v", err)
	}

	_, err := service.Reset(context.Background(), userID, first.ID, ResetInput{Reason: "again"})

	if !apperror.Is(err, "ACCOUNT_ARCHIVED") {
		t.Fatalf("resetting a sealed iteration err = %v, want ACCOUNT_ARCHIVED", err)
	}
}

func TestAccountsAreScopedToTheirOwner(t *testing.T) {
	t.Parallel()
	service, _, _, _ := newTestService()
	owner := uuid.New()
	entity := openTestAccount(t, service, owner)

	_, err := service.Get(context.Background(), uuid.New(), entity.ID)

	// Not-found rather than forbidden: confirming the ID exists would itself leak that somebody
	// else runs an account under it.
	if !apperror.Is(err, "ACCOUNT_NOT_FOUND") {
		t.Fatalf("cross-user Get() err = %v, want ACCOUNT_NOT_FOUND", err)
	}
}

func TestVerifyLedgerDetectsATamperedEntry(t *testing.T) {
	t.Parallel()
	service, _, ledger, _ := newTestService()
	userID := uuid.New()
	first := openTestAccount(t, service, userID)
	if _, err := service.Reset(context.Background(), userID, first.ID, ResetInput{Reason: "drawdown"}); err != nil {
		t.Fatalf("Reset() error = %v", err)
	}

	before, err := service.VerifyLedger(context.Background(), userID, first.ID)
	if err != nil {
		t.Fatalf("VerifyLedger() error = %v", err)
	}
	if !before.Valid || before.Entries != 2 || before.RootHash != before.StoredRootHash {
		t.Fatalf("a sealed, untouched ledger did not verify: %+v", before)
	}

	// Rewrite history the way a compromised database would: change a recorded balance and leave
	// every hash as it was.
	ledger.rows[first.ID][0].BalanceAfter = decimal.NewFromInt(999999)

	after, err := service.VerifyLedger(context.Background(), userID, first.ID)
	if err != nil {
		t.Fatalf("VerifyLedger() error = %v", err)
	}
	if after.Valid {
		t.Fatal("a tampered ledger verified as valid")
	}
	if after.BrokenAtSequence == nil || *after.BrokenAtSequence != 1 {
		t.Fatalf("broken sequence = %v, want the altered entry", after.BrokenAtSequence)
	}
}

func TestOpenRejectsADuplicateNameAndANonPositiveBalance(t *testing.T) {
	t.Parallel()
	service, _, _, _ := newTestService()
	userID := uuid.New()
	openTestAccount(t, service, userID)

	_, duplicate := service.Open(context.Background(), userID, OpenInput{Name: "Swing Replay", InitialBalance: decimal.NewFromInt(500)})
	_, zero := service.Open(context.Background(), userID, OpenInput{Name: "Scalp Drill", InitialBalance: decimal.Zero})

	if !apperror.Is(duplicate, "ACCOUNT_NAME_TAKEN") {
		t.Fatalf("duplicate name err = %v, want ACCOUNT_NAME_TAKEN", duplicate)
	}
	if !apperror.Is(zero, "VALIDATION_FAILED") {
		t.Fatalf("zero balance err = %v, want VALIDATION_FAILED", zero)
	}
}
