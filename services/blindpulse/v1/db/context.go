package db

import (
	"context"

	"gorm.io/gorm"
)

type contextKey struct{}

func WithContext(ctx context.Context, handle *gorm.DB) context.Context {
	return context.WithValue(ctx, contextKey{}, handle)
}

func FromContext(ctx context.Context, fallback *gorm.DB) *gorm.DB {
	if handle, ok := ctx.Value(contextKey{}).(*gorm.DB); ok && handle != nil {
		return handle
	}
	return fallback
}
