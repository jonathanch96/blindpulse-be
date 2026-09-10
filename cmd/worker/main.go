// Command worker runs the asynchronous half of BlindPulse: the outbox relay that moves recorded
// events onto Kafka, and (as projectors land) the consumers that build analytics from them. It is
// deployed as a separate process from the API so replay latency is never affected by a broker
// stall, and so the relay can be scaled independently of request traffic.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/jblabs/blindpulse-be/adapters/rest/config"
	"github.com/jblabs/blindpulse-be/pkg/cache"
	appkafka "github.com/jblabs/blindpulse-be/pkg/events/kafka"
	appLogger "github.com/jblabs/blindpulse-be/pkg/logger"
	accountsdb "github.com/jblabs/blindpulse-be/services/blindpulse/v1/db/blindpulse/accounts"
	feedsdb "github.com/jblabs/blindpulse-be/services/blindpulse/v1/db/blindpulse/blinded_feeds"
	instrumentsdb "github.com/jblabs/blindpulse-be/services/blindpulse/v1/db/blindpulse/instruments"
	barsdb "github.com/jblabs/blindpulse-be/services/blindpulse/v1/db/blindpulse/market_bars"
	outboxdb "github.com/jblabs/blindpulse-be/services/blindpulse/v1/db/blindpulse/outbox_events"
	sessionsdb "github.com/jblabs/blindpulse-be/services/blindpulse/v1/db/blindpulse/replay_sessions"
	feeddomain "github.com/jblabs/blindpulse-be/services/blindpulse/v1/domain/feed"
	sessiondomain "github.com/jblabs/blindpulse-be/services/blindpulse/v1/domain/session"
	"github.com/jblabs/blindpulse-be/services/blindpulse/v1/events/emitters"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	log := appLogger.New(cfg.App.Env)
	appLogger.SetDefault(log)

	db, err := config.NewDatabase(cfg, log)
	if err != nil {
		log.Error("worker startup failed", "err", err)
		os.Exit(1)
	}
	publisher, err := appkafka.NewPublisher(appkafka.Config{
		Brokers: cfg.Kafka.Brokers, ClientID: cfg.Kafka.ClientID, BatchSize: cfg.Kafka.BatchSize,
		BatchTimeout: cfg.Kafka.BatchTimeout, WriteTimeout: cfg.Kafka.WriteTimeout,
	})
	if err != nil {
		log.Error("kafka publisher failed", "err", err)
		os.Exit(1)
	}
	defer func() { _ = publisher.Close() }()
	if !publisher.Enabled() {
		// Without a broker the relay idles rather than exits: the process still holds its
		// database connection and starts draining the moment KAFKA_BROKERS is set and it restarts.
		log.Warn("kafka is not configured; the relay will idle and events stay queued in the outbox")
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// The idle-session sweeper. Without it, "one live session per account" — enforced by
	// replay_sessions_single_open — means a trader who closes their browser mid-replay cannot start
	// another session on that account, ever. The schema was built for this sweeper from the start:
	// the (status, last_active_at) index in migration 000011 has no other query to serve.
	redis, err := cache.New(ctx, cache.Config{
		Addr: cfg.Redis.Addr, Password: cfg.Redis.Password, DB: cfg.Redis.DB,
		Namespace: cfg.Redis.Namespace, DialTimeout: cfg.Redis.DialTimeout,
		ReadTimeout: cfg.Redis.ReadTimeout, PoolSize: cfg.Redis.PoolSize,
	})
	if err != nil {
		log.Warn("redis unavailable to the worker; swept sessions will not have their cached cursor cleared", "err", err)
	}
	feedService := feeddomain.NewService(feeddomain.Dependencies{
		Repo: feedsdb.New(db), Bars: barsdb.New(db), Instruments: instrumentsdb.New(db),
	})
	sessionService := sessiondomain.NewService(sessiondomain.Dependencies{
		Repo:     sessionsdb.New(db),
		State:    sessionsdb.NewRedisStateStore(redis, cfg.Redis.SessionTTL),
		Feeds:    feedService,
		Accounts: sessiondomain.NewAccountReader(accountsdb.New(db)),
		Outbox:   outboxdb.New(db),
		Topic:    cfg.Kafka.Topic,
		// No frame bus here: the worker abandons sessions, it does not drive them. A socket
		// attached to a swept session learns about it on its next poll or reconnect.
	})
	sweeper := emitters.NewSessionSweeper(sessionService, log, emitters.SweeperConfig{
		Interval:  cfg.Replay.SessionSweepInterval,
		IdleAfter: cfg.Replay.SessionIdleTimeout,
	})
	go sweeper.Run(ctx)

	relay := emitters.NewRelay(outboxdb.New(db), publisher, log, emitters.RelayConfig{
		Interval: cfg.Kafka.RelayInterval, BatchSize: cfg.Kafka.RelayBatch, MaxAttempts: cfg.Kafka.MaxAttempts,
	})
	log.Info("outbox relay started", "interval", cfg.Kafka.RelayInterval, "batch", cfg.Kafka.RelayBatch)
	if err := relay.Run(ctx); err != nil {
		log.Error("relay stopped", "err", err)
		os.Exit(1)
	}
	log.Info("worker stopped")
}
