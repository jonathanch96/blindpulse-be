package config

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"github.com/jblabs/blindpulse-be/pkg/middleware"
	appvalidator "github.com/jblabs/blindpulse-be/pkg/validator"
)

func NewServer(cfg *Config, log *slog.Logger) *gin.Engine {
	if cfg.IsProduction() {
		gin.SetMode(gin.ReleaseMode)
	}
	binding.Validator = appvalidator.New()
	engine := gin.New()
	engine.Use(
		middleware.RequestID(),
		middleware.Logging(log),
		middleware.Recovery(log),
		middleware.CORS(middleware.CORSOptions{AllowedOrigins: cfg.CORS.AllowedOrigins, Production: cfg.IsProduction()}),
	)
	return engine
}

func Run(ctx context.Context, cfg *Config, engine *gin.Engine) error {
	return run(ctx, cfg, engine, nil)
}

func run(ctx context.Context, cfg *Config, engine *gin.Engine, started chan<- net.Addr) error {
	ctx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	server := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.App.Port),
		Handler:           engine,
		ReadHeaderTimeout: 5 * time.Second,
	}
	listener, err := net.Listen("tcp", server.Addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", server.Addr, err)
	}
	errCh := make(chan error, 1)
	go func() { errCh <- server.Serve(listener) }()
	if started != nil {
		started <- listener.Addr()
	}

	select {
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.App.ShutdownTimeout)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			_ = server.Close()
			return fmt.Errorf("graceful shutdown: %w", err)
		}
		return nil
	}
}
