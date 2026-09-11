package session_reveals

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/jblabs/blindpulse-be/pkg/apperror"
	appdb "github.com/jblabs/blindpulse-be/services/blindpulse/v1/db"
	domainreveal "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/reveal"
	"gorm.io/gorm"
)

type adapterGormPostgresql struct{ db *gorm.DB }

func NewGormPostgresqlAdapter(db *gorm.DB) *adapterGormPostgresql {
	return &adapterGormPostgresql{db: db}
}

func New(db *gorm.DB) *adapterGormPostgresql { return NewGormPostgresqlAdapter(db) }

func (a *adapterGormPostgresql) Create(ctx context.Context, entity *domainreveal.Reveal) (*domainreveal.Reveal, error) {
	model := fromDomain(*entity)
	if err := appdb.FromContext(ctx, a.db).WithContext(ctx).Create(&model).Error; err != nil {
		// session_id is the primary key, so two concurrent reveals race here and exactly one wins.
		// The loser is told the truth rather than an internal error: it has already been revealed.
		if isUniqueViolation(err) {
			return nil, apperror.New("ALREADY_REVEALED")
		}
		return nil, apperror.Wrap(err, "INTERNAL_ERROR")
	}
	created := toDomain(model)
	return &created, nil
}

// GetBySessionID answers REVEAL_LOCKED rather than a not-found for a session with no reveal yet.
// The session exists and the caller owns it; what they are being told is that the curtain has not
// gone up, which is a different fact from "no such session".
func (a *adapterGormPostgresql) GetBySessionID(ctx context.Context, sessionID uuid.UUID) (*domainreveal.Reveal, error) {
	var model SessionReveal
	err := appdb.FromContext(ctx, a.db).WithContext(ctx).Where("session_id = ?", sessionID).First(&model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, apperror.New("REVEAL_LOCKED")
	}
	if err != nil {
		return nil, apperror.Wrap(err, "INTERNAL_ERROR")
	}
	entity := toDomain(model)
	return &entity, nil
}

func isUniqueViolation(err error) bool {
	return strings.Contains(err.Error(), "23505") || strings.Contains(err.Error(), "duplicate key")
}
