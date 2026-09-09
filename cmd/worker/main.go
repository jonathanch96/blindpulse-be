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
	appkafka "github.com/jblabs/blindpulse-be/pkg/events/kafka"
	appLogger "github.com/jblabs/blindpulse-be/pkg/logger"
	outboxdb "github.com/jblabs/blindpulse-be/services/blindpulse/v1/db/blindpulse/outbox_events"
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
