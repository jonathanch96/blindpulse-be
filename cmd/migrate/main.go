package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jblabs/blindpulse-be/adapters/rest/config"
	_ "github.com/lib/pq"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		fatal(err)
	}

	databaseURL := cfg.DB.MigrationURL()
	if err := waitForDatabase(databaseURL, 60*time.Second); err != nil {
		fatal(err)
	}

	migrator, err := migrate.New("file:///migrations", databaseURL)
	if err != nil {
		fatal(fmt.Errorf("create migrator: %w", err))
	}
	defer func() { _, _ = migrator.Close() }()

	if err := migrator.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		fatal(fmt.Errorf("apply migrations: %w", err))
	}
}

func waitForDatabase(databaseURL string, timeout time.Duration) error {
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer func() { _ = db.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		pingCtx, pingCancel := context.WithTimeout(ctx, 2*time.Second)
		err = db.PingContext(pingCtx)
		pingCancel()
		if err == nil {
			return nil
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("database was not ready within %s: %w", timeout, err)
		case <-ticker.C:
		}
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
