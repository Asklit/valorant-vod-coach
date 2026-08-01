package app

import (
	"context"
	"time"
)

type AuthSession struct {
	Token     string
	CSRFToken string
	User      PublicAuthUser
	ExpiresAt time.Time
}

type AuthSessionStore interface {
	Create(ctx context.Context, user PublicAuthUser, ttl time.Duration) (AuthSession, error)
	Get(ctx context.Context, token string) (AuthSession, bool, error)
	Delete(ctx context.Context, token string) error
}
