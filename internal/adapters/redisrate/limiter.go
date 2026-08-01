package redisrate

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/asklit/valorant-vod-coach/internal/app"
)

type Limiter struct {
	Client *redis.Client
	Prefix string
}

func (l Limiter) Allow(ctx context.Context, key string, limit int, window time.Duration) (bool, time.Duration, error) {
	if l.Client == nil {
		return false, 0, errors.New("redis rate limiter requires client")
	}
	if limit <= 0 {
		return true, 0, nil
	}
	if window <= 0 {
		window = time.Minute
	}
	prefix := l.Prefix
	if prefix == "" {
		prefix = "vodcoach:rate:"
	}
	result, err := fixedWindowScript.Run(
		ctx,
		l.Client,
		[]string{prefix + key},
		strconv.Itoa(limit),
		strconv.FormatInt(window.Milliseconds(), 10),
	).Slice()
	if err != nil {
		return false, 0, fmt.Errorf("apply Redis rate limit: %w", err)
	}
	if len(result) != 2 {
		return false, 0, fmt.Errorf("unexpected Redis rate limit result")
	}
	allowed, err := redisInt(result[0])
	if err != nil {
		return false, 0, err
	}
	retryMillis, err := redisInt(result[1])
	if err != nil {
		return false, 0, err
	}
	if retryMillis < 0 {
		retryMillis = window.Milliseconds()
	}
	return allowed == 1, time.Duration(retryMillis) * time.Millisecond, nil
}

func redisInt(value any) (int64, error) {
	switch typed := value.(type) {
	case int64:
		return typed, nil
	case string:
		return strconv.ParseInt(typed, 10, 64)
	case []byte:
		return strconv.ParseInt(string(typed), 10, 64)
	default:
		return 0, fmt.Errorf("unexpected Redis integer type %T", value)
	}
}

var fixedWindowScript = redis.NewScript(`
local count = redis.call('INCR', KEYS[1])
if count == 1 then
  redis.call('PEXPIRE', KEYS[1], ARGV[2])
end
local ttl = redis.call('PTTL', KEYS[1])
if count > tonumber(ARGV[1]) then
  return {0, ttl}
end
return {1, ttl}
`)

var _ app.RateLimiter = Limiter{}
