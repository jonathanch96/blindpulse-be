package replay_sessions

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jblabs/blindpulse-be/pkg/apperror"
	appdb "github.com/jblabs/blindpulse-be/services/blindpulse/v1/db"
	domainsession "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/session"
	"gorm.io/gorm"
)

type adapterGormPostgresql struct{ db *gorm.DB }

func NewGormPostgresqlAdapter(db *gorm.DB) *adapterGormPostgresql {
	return &adapterGormPostgresql{db: db}
}

func New(db *gorm.DB) *adapterGormPostgresql { return NewGormPostgresqlAdapter(db) }

func (a *adapterGormPostgresql) Create(ctx context.Context, entity *domainsession.Session) (*domainsession.Session, error) {
	model := fromDomain(*entity)
	if err := appdb.FromContext(ctx, a.db).WithContext(ctx).Create(&model).Error; err != nil {
		// The partial unique index is the real guard against two live sessions on one account;
		// translate its violation into the error that names the actual problem.
		if isUniqueViolation(err) {
			return nil, apperror.New("SESSION_ALREADY_OPEN")
		}
		return nil, apperror.Wrap(err, "INTERNAL_ERROR")
	}
	created := toDomain(model)
	return &created, nil
}

func (a *adapterGormPostgresql) GetByID(ctx context.Context, id uuid.UUID) (*domainsession.Session, error) {
	var model ReplaySession
	err := appdb.FromContext(ctx, a.db).WithContext(ctx).Where("id = ?", id).First(&model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, apperror.New("SESSION_NOT_FOUND")
	}
	if err != nil {
		return nil, apperror.Wrap(err, "INTERNAL_ERROR")
	}
	entity := toDomain(model)
	return &entity, nil
}

func (a *adapterGormPostgresql) ListLiveByUserID(ctx context.Context, userID uuid.UUID) ([]domainsession.Session, error) {
	var models []ReplaySession
	err := appdb.FromContext(ctx, a.db).WithContext(ctx).
		Where("user_id = ? AND status IN ?", userID, []string{string(domainsession.StatusOpen), string(domainsession.StatusPaused)}).
		Order("last_active_at DESC").Find(&models).Error
	if err != nil {
		return nil, apperror.Wrap(err, "INTERNAL_ERROR")
	}
	return toDomains(models), nil
}

func (a *adapterGormPostgresql) GetLiveByAccountID(ctx context.Context, accountID uuid.UUID) (*domainsession.Session, error) {
	var model ReplaySession
	err := appdb.FromContext(ctx, a.db).WithContext(ctx).
		Where("account_id = ? AND status IN ?", accountID, []string{string(domainsession.StatusOpen), string(domainsession.StatusPaused)}).
		First(&model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, apperror.New("SESSION_NOT_FOUND")
	}
	if err != nil {
		return nil, apperror.Wrap(err, "INTERNAL_ERROR")
	}
	entity := toDomain(model)
	return &entity, nil
}

// UpdateCursor is optimistically locked. Two concurrent steps on one session would otherwise be
// able to interleave and lose a reveal, which is the one direction the cursor must never move.
func (a *adapterGormPostgresql) UpdateCursor(ctx context.Context, entity *domainsession.Session) error {
	result := appdb.FromContext(ctx, a.db).WithContext(ctx).Model(&ReplaySession{}).
		Where("id = ? AND version = ?", entity.ID, entity.Version).
		Updates(map[string]any{
			"status":                string(entity.Status),
			"timeframe":             string(entity.Timeframe),
			"playback_speed":        entity.Speed,
			"cursor_index":          entity.CursorIndex,
			"last_checkpoint_index": entity.LastCheckpointIndex,
			"last_active_at":        entity.LastActiveAt,
			"closed_at":             entity.ClosedAt,
			"version":               gorm.Expr("version + 1"),
			"updated_at":            gorm.Expr("now()"),
		})
	if result.Error != nil {
		return apperror.Wrap(result.Error, "INTERNAL_ERROR")
	}
	if result.RowsAffected == 0 {
		return apperror.New("CONCURRENT_MODIFICATION")
	}
	entity.Version++
	return nil
}

func (a *adapterGormPostgresql) UpdateStatus(ctx context.Context, id uuid.UUID, status domainsession.Status, version int) error {
	values := map[string]any{
		"status":     string(status),
		"version":    gorm.Expr("version + 1"),
		"updated_at": gorm.Expr("now()"),
	}
	if status == domainsession.StatusClosed || status == domainsession.StatusAbandoned {
		values["closed_at"] = gorm.Expr("now()")
	}
	result := appdb.FromContext(ctx, a.db).WithContext(ctx).Model(&ReplaySession{}).
		Where("id = ? AND version = ?", id, version).Updates(values)
	if result.Error != nil {
		return apperror.Wrap(result.Error, "INTERNAL_ERROR")
	}
	if result.RowsAffected == 0 {
		return apperror.New("CONCURRENT_MODIFICATION")
	}
	return nil
}

// AbandonIdle claims and flips idle sessions in one statement.
//
// One statement rather than a read followed by a write, because two worker replicas sweeping at the
// same moment would otherwise both select the same rows and both emit an abandonment event for
// each. `UPDATE ... WHERE status IN (...)` lets PostgreSQL settle it: the second writer sees rows
// that no longer match its predicate and claims nothing.
//
// The subquery with LIMIT bounds one sweep so a backlog after an outage is drained across ticks
// rather than taken in one long-held lock. It is ordered by last_active_at so the most stale
// sessions — the ones most likely to be holding an account hostage — go first.
func (a *adapterGormPostgresql) AbandonIdle(ctx context.Context, cutoff time.Time, limit int) ([]domainsession.Session, error) {
	if limit < 1 {
		limit = 100
	}
	var claimed []ReplaySession
	result := appdb.FromContext(ctx, a.db).WithContext(ctx).Raw(`
		UPDATE blindpulse.replay_sessions
		   SET status = ?, closed_at = now(), updated_at = now(), version = version + 1
		 WHERE id IN (
		       SELECT id FROM blindpulse.replay_sessions
		        WHERE status IN (?, ?) AND last_active_at < ?
		        ORDER BY last_active_at
		        LIMIT ?
		       )
		RETURNING *`,
		string(domainsession.StatusAbandoned),
		string(domainsession.StatusOpen), string(domainsession.StatusPaused),
		cutoff, limit,
	).Scan(&claimed)
	if result.Error != nil {
		return nil, apperror.Wrap(result.Error, "INTERNAL_ERROR")
	}
	return toDomains(claimed), nil
}

// isUniqueViolation matches on the SQLSTATE rather than the driver's error type, so it keeps
// working if the pgx wrapper changes shape underneath GORM.
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23505"
	}
	return strings.Contains(err.Error(), "23505")
}
