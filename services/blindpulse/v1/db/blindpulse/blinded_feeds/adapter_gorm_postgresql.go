package blinded_feeds

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jblabs/blindpulse-be/pkg/apperror"
	appdb "github.com/jblabs/blindpulse-be/services/blindpulse/v1/db"
	feeddomain "github.com/jblabs/blindpulse-be/services/blindpulse/v1/domain/feed"
	domainfeed "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/feed"
	"gorm.io/gorm"
)

type adapterGormPostgresql struct{ db *gorm.DB }

func NewGormPostgresqlAdapter(db *gorm.DB) *adapterGormPostgresql {
	return &adapterGormPostgresql{db: db}
}

func New(db *gorm.DB) *adapterGormPostgresql { return NewGormPostgresqlAdapter(db) }

func (a *adapterGormPostgresql) Create(ctx context.Context, entity *domainfeed.Feed) (*domainfeed.Feed, error) {
	model := fromDomain(*entity)
	if model.Version == 0 {
		model.Version = 1
	}
	if err := appdb.FromContext(ctx, a.db).WithContext(ctx).Create(&model).Error; err != nil {
		return nil, apperror.Wrap(err, "INTERNAL_ERROR")
	}
	created := toDomain(model)
	return &created, nil
}

func (a *adapterGormPostgresql) GetByID(ctx context.Context, id uuid.UUID) (*domainfeed.Feed, error) {
	var model BlindedFeed
	err := appdb.FromContext(ctx, a.db).WithContext(ctx).Where("id = ?", id).First(&model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, apperror.New("FEED_NOT_FOUND")
	}
	if err != nil {
		return nil, apperror.Wrap(err, "INTERNAL_ERROR")
	}
	entity := toDomain(model)
	return &entity, nil
}

func (a *adapterGormPostgresql) List(ctx context.Context, filter feeddomain.ListFilter) ([]domainfeed.Feed, error) {
	var models []BlindedFeed
	query := a.filtered(ctx, filter)
	if err := query.Order("built_at DESC").Limit(filter.Limit).Find(&models).Error; err != nil {
		return nil, apperror.Wrap(err, "INTERNAL_ERROR")
	}
	return toDomains(models), nil
}

// ListExcludingTraded is the query behind "randomize": published feeds this user has never opened
// a session against. Handing back a window whose ending they already know would reintroduce the
// hindsight the whole product removes, so the exclusion is part of the query rather than a filter
// applied afterwards that somebody could forget.
func (a *adapterGormPostgresql) ListExcludingTraded(ctx context.Context, userID uuid.UUID,
	filter feeddomain.ListFilter) ([]domainfeed.Feed, error) {
	var models []BlindedFeed
	query := a.filtered(ctx, filter).
		Where("NOT EXISTS (SELECT 1 FROM blindpulse.replay_sessions s WHERE s.feed_id = blindpulse.blinded_feeds.id AND s.user_id = ?)", userID)
	if err := query.Order("built_at DESC").Limit(filter.Limit).Find(&models).Error; err != nil {
		return nil, apperror.Wrap(err, "INTERNAL_ERROR")
	}
	return toDomains(models), nil
}

func (a *adapterGormPostgresql) ExistsByAlias(ctx context.Context, alias string) (bool, error) {
	var count int64
	err := appdb.FromContext(ctx, a.db).WithContext(ctx).Model(&BlindedFeed{}).
		Where("alias_label = ?", alias).Count(&count).Error
	if err != nil {
		return false, apperror.Wrap(err, "INTERNAL_ERROR")
	}
	return count > 0, nil
}

// NextAliasNumber draws from a sequence rather than computing max()+1, so two builders running
// concurrently cannot mint the same alias.
func (a *adapterGormPostgresql) NextAliasNumber(ctx context.Context) (int64, error) {
	var number int64
	err := appdb.FromContext(ctx, a.db).WithContext(ctx).
		Raw("SELECT nextval('blindpulse.feed_alias_seq')").Scan(&number).Error
	if err != nil {
		return 0, apperror.Wrap(err, "INTERNAL_ERROR")
	}
	return number, nil
}

func (a *adapterGormPostgresql) filtered(ctx context.Context, filter feeddomain.ListFilter) *gorm.DB {
	query := appdb.FromContext(ctx, a.db).WithContext(ctx).Model(&BlindedFeed{}).
		Where("is_published = ?", true)
	if filter.Difficulty != "" {
		query = query.Where("difficulty = ?", string(filter.Difficulty))
	}
	if filter.BaseTimeframe != "" {
		query = query.Where("base_timeframe = ?", string(filter.BaseTimeframe))
	}
	return query
}
