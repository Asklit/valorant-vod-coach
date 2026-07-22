package redislock

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/asklit/valorant-vod-coach/internal/app"
)

type Manager struct {
	Client *redis.Client
	Prefix string
}

type Lock struct {
	client *redis.Client
	key    string
	token  string
}

func NewManager(redisURL string) (Manager, error) {
	options, err := redis.ParseURL(redisURL)
	if err != nil {
		return Manager{}, err
	}
	return Manager{Client: redis.NewClient(options), Prefix: "vodcoach:"}, nil
}

func (m Manager) Close() error {
	if m.Client == nil {
		return nil
	}
	return m.Client.Close()
}

func (m Manager) Acquire(ctx context.Context, key string, ttl time.Duration) (app.Lock, error) {
	if m.Client == nil {
		return nil, fmt.Errorf("redis lock manager requires client")
	}
	if ttl <= 0 {
		ttl = time.Hour
	}
	token, err := randomToken()
	if err != nil {
		return nil, err
	}
	fullKey := m.Prefix + key
	acquired, err := m.Client.SetNX(ctx, fullKey, token, ttl).Result()
	if err != nil {
		return nil, err
	}
	if !acquired {
		return nil, app.LockAlreadyHeldError{Key: key}
	}
	return Lock{client: m.Client, key: fullKey, token: token}, nil
}

func (l Lock) Release(ctx context.Context) error {
	if l.client == nil || l.key == "" || l.token == "" {
		return nil
	}
	result, err := releaseScript.Run(ctx, l.client, []string{l.key}, l.token).Int()
	if err != nil {
		return err
	}
	if result == 0 {
		return nil
	}
	return nil
}

var releaseScript = redis.NewScript(`
if redis.call("get", KEYS[1]) == ARGV[1] then
  return redis.call("del", KEYS[1])
end
return 0
`)

func randomToken() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", errors.New("generate redis lock token")
	}
	return hex.EncodeToString(raw[:]), nil
}
