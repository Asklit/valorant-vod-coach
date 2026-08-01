package redissession

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/asklit/valorant-vod-coach/internal/app"
)

type Store struct {
	Client *redis.Client
	Prefix string
	Clock  func() time.Time
}

type sessionRecord struct {
	CSRFToken string             `json:"csrf_token"`
	User      app.PublicAuthUser `json:"user"`
	ExpiresAt time.Time          `json:"expires_at"`
}

func New(redisURL string) (*Store, error) {
	options, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, err
	}
	return &Store{Client: redis.NewClient(options), Prefix: "vodcoach:session:"}, nil
}

func (s *Store) Close() error {
	if s == nil || s.Client == nil {
		return nil
	}
	return s.Client.Close()
}

func (s *Store) Ping(ctx context.Context) error {
	if s == nil || s.Client == nil {
		return errors.New("redis session store requires client")
	}
	return s.Client.Ping(ctx).Err()
}

func (s *Store) Create(ctx context.Context, user app.PublicAuthUser, ttl time.Duration) (app.AuthSession, error) {
	if s == nil || s.Client == nil {
		return app.AuthSession{}, errors.New("redis session store requires client")
	}
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	token, err := app.NewAuthToken()
	if err != nil {
		return app.AuthSession{}, err
	}
	csrfToken, err := app.NewAuthToken()
	if err != nil {
		return app.AuthSession{}, err
	}
	expiresAt := s.now().Add(ttl)
	record, err := json.Marshal(sessionRecord{CSRFToken: csrfToken, User: user, ExpiresAt: expiresAt})
	if err != nil {
		return app.AuthSession{}, fmt.Errorf("encode session: %w", err)
	}
	if err := s.Client.Set(ctx, s.key(token), record, ttl).Err(); err != nil {
		return app.AuthSession{}, fmt.Errorf("store session: %w", err)
	}
	return app.AuthSession{Token: token, CSRFToken: csrfToken, User: user, ExpiresAt: expiresAt}, nil
}

func (s *Store) Get(ctx context.Context, token string) (app.AuthSession, bool, error) {
	if s == nil || s.Client == nil {
		return app.AuthSession{}, false, errors.New("redis session store requires client")
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return app.AuthSession{}, false, nil
	}
	raw, err := s.Client.Get(ctx, s.key(token)).Bytes()
	if errors.Is(err, redis.Nil) {
		return app.AuthSession{}, false, nil
	}
	if err != nil {
		return app.AuthSession{}, false, fmt.Errorf("load session: %w", err)
	}
	var record sessionRecord
	if err := json.Unmarshal(raw, &record); err != nil {
		_ = s.Client.Del(ctx, s.key(token)).Err()
		return app.AuthSession{}, false, fmt.Errorf("decode session: %w", err)
	}
	if !record.ExpiresAt.After(s.now()) {
		_ = s.Client.Del(ctx, s.key(token)).Err()
		return app.AuthSession{}, false, nil
	}
	return app.AuthSession{
		Token: token, CSRFToken: record.CSRFToken, User: record.User, ExpiresAt: record.ExpiresAt,
	}, true, nil
}

func (s *Store) Delete(ctx context.Context, token string) error {
	if s == nil || s.Client == nil {
		return errors.New("redis session store requires client")
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return nil
	}
	if err := s.Client.Del(ctx, s.key(token)).Err(); err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

func (s *Store) key(token string) string {
	digest := sha256.Sum256([]byte(token))
	prefix := s.Prefix
	if prefix == "" {
		prefix = "vodcoach:session:"
	}
	return prefix + hex.EncodeToString(digest[:])
}

func (s *Store) now() time.Time {
	if s.Clock != nil {
		return s.Clock().UTC()
	}
	return time.Now().UTC()
}

var _ app.AuthSessionStore = (*Store)(nil)
