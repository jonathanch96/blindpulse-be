package logger

import (
	"context"
	"io"
	"log/slog"
	"os"
	"strings"
)

type contextKey string

const (
	traceIDKey contextKey = "trace_id"
	userIDKey  contextKey = "user_id"
)

func New(env string) *slog.Logger {
	var handler slog.Handler
	options := &slog.HandlerOptions{Level: slog.LevelInfo, ReplaceAttr: redactAttr}
	if env == "staging" || env == "production" {
		handler = slog.NewJSONHandler(os.Stdout, options)
	} else {
		handler = slog.NewTextHandler(os.Stdout, options)
	}
	return slog.New(handler)
}

func WithTrace(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, traceIDKey, id)
}

func WithUser(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, userIDKey, id)
}

func TraceID(ctx context.Context) string {
	id, _ := ctx.Value(traceIDKey).(string)
	return id
}

func UserID(ctx context.Context) string {
	id, _ := ctx.Value(userIDKey).(string)
	return id
}

func FromContext(ctx context.Context) *slog.Logger {
	log := slog.Default()
	if id := TraceID(ctx); id != "" {
		log = log.With("trace_id", id)
	}
	if id := UserID(ctx); id != "" {
		log = log.With("user_id", id)
	}
	return log
}

func SetDefault(log *slog.Logger) { slog.SetDefault(log) }

func Redact(values map[string]any) map[string]any {
	clean := make(map[string]any, len(values))
	for key, value := range values {
		if sensitive(key) {
			clean[key] = "[REDACTED]"
		} else {
			clean[key] = value
		}
	}
	return clean
}

func redactAttr(_ []string, attr slog.Attr) slog.Attr {
	if sensitive(attr.Key) {
		return slog.String(attr.Key, "[REDACTED]")
	}
	return attr
}

func sensitive(key string) bool {
	key = strings.ToLower(strings.ReplaceAll(key, "-", "_"))
	return key == "password" || key == "token" || key == "authorization" || key == "account_number"
}

func NewForTest(w io.Writer) *slog.Logger {
	return slog.New(slog.NewJSONHandler(w, nil))
}
