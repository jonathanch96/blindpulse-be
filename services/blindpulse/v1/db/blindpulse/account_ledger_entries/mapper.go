package account_ledger_entries

import (
	domainaccount "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/account"
)

func fromDomain(entity domainaccount.LedgerEntry) LedgerEntry {
	return LedgerEntry{
		ID: entity.ID, AccountID: entity.AccountID, Sequence: entity.Sequence, Kind: string(entity.Kind),
		ReferenceType: entity.ReferenceType, ReferenceID: entity.ReferenceID, Amount: entity.Amount,
		BalanceAfter: entity.BalanceAfter, EquityAfter: entity.EquityAfter, Payload: entity.Payload,
		PreviousHash: entity.PreviousHash, EntryHash: entity.EntryHash, RecordedAt: entity.RecordedAt,
	}
}

func toDomain(model LedgerEntry) domainaccount.LedgerEntry {
	return domainaccount.LedgerEntry{
		ID: model.ID, AccountID: model.AccountID, Sequence: model.Sequence,
		Kind: domainaccount.LedgerKind(model.Kind), ReferenceType: model.ReferenceType,
		ReferenceID: model.ReferenceID, Amount: model.Amount, BalanceAfter: model.BalanceAfter,
		EquityAfter: model.EquityAfter, Payload: model.Payload, PreviousHash: model.PreviousHash,
		EntryHash: model.EntryHash, RecordedAt: model.RecordedAt,
	}
}
