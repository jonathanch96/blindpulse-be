package config

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

func NewDatabase(cfg *Config, log *slog.Logger) (*gorm.DB, error) {
	if cfg.DB.AutoMigrate {
		if err := RunMigrations(cfg); err != nil {
			return nil, err
		}
	}
	db, err := gorm.Open(postgres.Open(cfg.DB.DSN()), &gorm.Config{
		Logger: newGormLogger(log),
	})
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("access database pool: %w", err)
	}
	sqlDB.SetMaxOpenConns(cfg.DB.MaxOpenConns)
	sqlDB.SetMaxIdleConns(cfg.DB.MaxIdleConns)
	sqlDB.SetConnMaxLifetime(cfg.DB.ConnMaxLifetime)
	log.Info("database pool configured",
		"max_open_conns", cfg.DB.MaxOpenConns,
		"max_idle_conns", cfg.DB.MaxIdleConns,
		"conn_max_lifetime", cfg.DB.ConnMaxLifetime,
		"schema", cfg.DB.Schema,
	)
	return db, nil
}

func RunMigrations(cfg *Config) error {
	m, err := migrate.New("file://migrations", cfg.DB.MigrationURL())
	if err != nil {
		return fmt.Errorf("initialize migrations: %w", err)
	}
	defer func() { _, _ = m.Close() }()
	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("run migrations: %w", err)
	}
	return nil
}

func Ping(ctx context.Context, db *gorm.DB) error {
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	return sqlDB.PingContext(ctx)
}

type slogGormLogger struct {
	log       *slog.Logger
	level     gormlogger.LogLevel
	slowQuery time.Duration
}

func newGormLogger(log *slog.Logger) gormlogger.Interface {
	return &slogGormLogger{log: log, level: gormlogger.Warn, slowQuery: 200 * time.Millisecond}
}

func (l *slogGormLogger) LogMode(level gormlogger.LogLevel) gormlogger.Interface {
	copy := *l
	copy.level = level
	return &copy
}

func (l *slogGormLogger) Info(ctx context.Context, message string, args ...any) {
	if l.level >= gormlogger.Info {
		l.log.InfoContext(ctx, fmt.Sprintf(message, args...))
	}
}

func (l *slogGormLogger) Warn(ctx context.Context, message string, args ...any) {
	if l.level >= gormlogger.Warn {
		l.log.WarnContext(ctx, fmt.Sprintf(message, args...))
	}
}

func (l *slogGormLogger) Error(ctx context.Context, message string, args ...any) {
	if l.level >= gormlogger.Error {
		l.log.ErrorContext(ctx, fmt.Sprintf(message, args...))
	}
}

func (l *slogGormLogger) Trace(ctx context.Context, begin time.Time, query func() (string, int64), err error) {
	if l.level == gormlogger.Silent {
		return
	}
	elapsed := time.Since(begin)
	sql, rows := query()
	attrs := []any{"elapsed_ms", elapsed.Milliseconds(), "rows", rows, "sql", sql}
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) && l.level >= gormlogger.Error {
		l.log.ErrorContext(ctx, "database query failed", append(attrs, "err", err)...)
	} else if elapsed > l.slowQuery && l.level >= gormlogger.Warn {
		l.log.WarnContext(ctx, "slow database query", attrs...)
	} else if l.level >= gormlogger.Info {
		l.log.DebugContext(ctx, "database query", attrs...)
	}
}
