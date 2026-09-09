package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/jblabs/blindpulse-be/adapters/rest/config"
	"github.com/jblabs/blindpulse-be/adapters/rest/handlers"
	_ "github.com/jblabs/blindpulse-be/docs"
	"github.com/jblabs/blindpulse-be/pkg/cache"
	appLogger "github.com/jblabs/blindpulse-be/pkg/logger"
	blindpulse "github.com/jblabs/blindpulse-be/services/blindpulse/v1/controllers"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
	"gorm.io/gorm"
)

// @title BlindPulse Replay Lab API
// @version 1.0
// @description Zero-hindsight blinded market replay, execution simulation, and non-destructive account reset trees.
// @host localhost:8080
// @BasePath /api/v1
// @securityDefinitions.apikey BearerAuth
// @in header
// @name Authorization
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
		log.Error("startup failed", "err", err)
		os.Exit(1)
	}
	ctx := context.Background()
	redis, err := cache.New(ctx, cache.Config{
		Addr: cfg.Redis.Addr, Password: cfg.Redis.Password, DB: cfg.Redis.DB,
		Namespace: cfg.Redis.Namespace, DialTimeout: cfg.Redis.DialTimeout,
		ReadTimeout: cfg.Redis.ReadTimeout, PoolSize: cfg.Redis.PoolSize,
	})
	if err != nil {
		// Redis is configured but unreachable. Starting anyway would serve replay from process
		// memory behind a load balancer that assumes shared state, so fail loudly instead.
		log.Error("redis startup failed", "err", err)
		os.Exit(1)
	}
	defer func() { _ = redis.Close() }()
	if !redis.Enabled() {
		log.Warn("redis is not configured; replay state is per-instance and will not scale out")
	}
	if !cfg.KafkaEnabled() {
		log.Warn("kafka is not configured; events accumulate in the outbox until a broker is set")
	}
	engine := config.NewServer(cfg, log)
	registerRoutes(engine, cfg, db, log, redis)
	if err := config.Run(ctx, cfg, engine); err != nil {
		log.Error("server stopped", "err", err)
		os.Exit(1)
	}
}

func registerRoutes(engine *gin.Engine, cfg *config.Config, db *gorm.DB, log *slog.Logger, redis *cache.Client) {
	engine.GET("/healthz", handlers.Health)
	engine.GET("/readyz", handlers.Ready(db, redis))
	engine.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))
	v1 := engine.Group("/api/v1")
	blindpulse.NewService(blindpulse.Dependencies{DB: db, Cfg: cfg, Log: log, Cache: redis}).RegisterRoutes(v1)
}
