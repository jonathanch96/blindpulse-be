package replay_sessions

import (
	"github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/market"
	domainsession "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/session"
)

func fromDomain(entity domainsession.Session) ReplaySession {
	return ReplaySession{
		ID: entity.ID, UserID: entity.UserID, AccountID: entity.AccountID, FeedID: entity.FeedID,
		Status: string(entity.Status), Timeframe: string(entity.Timeframe), PlaybackSpeed: entity.Speed,
		CursorIndex: entity.CursorIndex, RevealedIndex: entity.RevealedIndex, CursorAt: entity.CursorAt,
		Seed: entity.Seed, LastCheckpointIndex: entity.LastCheckpointIndex,
		StartedAt: entity.StartedAt, LastActiveAt: entity.LastActiveAt,
		ClosedAt: entity.ClosedAt, RevealedAt: entity.RevealedAt, RootHash: entity.RootHash,
		CreatedAt: entity.CreatedAt, UpdatedAt: entity.UpdatedAt, Version: entity.Version,
	}
}

func toDomain(model ReplaySession) domainsession.Session {
	return domainsession.Session{
		ID: model.ID, UserID: model.UserID, AccountID: model.AccountID, FeedID: model.FeedID,
		Status: domainsession.Status(model.Status), Timeframe: market.Timeframe(model.Timeframe),
		Speed: model.PlaybackSpeed, CursorIndex: model.CursorIndex, RevealedIndex: model.RevealedIndex,
		CursorAt: model.CursorAt, Seed: model.Seed, LastCheckpointIndex: model.LastCheckpointIndex,
		StartedAt: model.StartedAt, LastActiveAt: model.LastActiveAt,
		ClosedAt: model.ClosedAt, RevealedAt: model.RevealedAt, RootHash: model.RootHash,
		CreatedAt: model.CreatedAt, UpdatedAt: model.UpdatedAt, Version: model.Version,
	}
}

func toDomains(models []ReplaySession) []domainsession.Session {
	sessions := make([]domainsession.Session, 0, len(models))
	for _, model := range models {
		sessions = append(sessions, toDomain(model))
	}
	return sessions
}
