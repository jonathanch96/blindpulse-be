package identity

import (
	"context"

	"github.com/google/uuid"
)

type Identity struct {
	UserID uuid.UUID
	Email  string
	Name   string
}

type contextKey struct{}

func WithContext(ctx context.Context, value Identity) context.Context {
	return context.WithValue(ctx, contextKey{}, value)
}

func FromContext(ctx context.Context) (Identity, bool) {
	value, ok := ctx.Value(contextKey{}).(Identity)
	return value, ok
}

func MustFromContext(ctx context.Context) Identity {
	value, ok := FromContext(ctx)
	if !ok {
		panic("identity is missing from context")
	}
	return value
}
