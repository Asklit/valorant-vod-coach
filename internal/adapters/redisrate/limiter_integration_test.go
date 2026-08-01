package redisrate

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

func TestRedisRateLimiterIntegration(t *testing.T) {
	redisURL := strings.TrimSpace(os.Getenv("TEST_REDIS_URL"))
	if redisURL == "" {
		t.Skip("TEST_REDIS_URL is not set")
	}
	options, err := redis.ParseURL(redisURL)
	if err != nil {
		t.Fatalf("parse Redis URL: %v", err)
	}
	client := redis.NewClient(options)
	defer client.Close()
	prefix := "vodcoach:test:rate:" + time.Now().UTC().Format("20060102150405.000000000") + ":"
	limiter := Limiter{Client: client, Prefix: prefix}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	defer client.Del(context.Background(), prefix+"login:test-client")

	for attempt := 0; attempt < 2; attempt++ {
		allowed, _, err := limiter.Allow(ctx, "login:test-client", 2, time.Minute)
		if err != nil || !allowed {
			t.Fatalf("attempt %d should be allowed: allowed=%v err=%v", attempt+1, allowed, err)
		}
	}
	allowed, retryAfter, err := limiter.Allow(ctx, "login:test-client", 2, time.Minute)
	if err != nil || allowed || retryAfter <= 0 {
		t.Fatalf("third attempt must be limited: allowed=%v retry=%s err=%v", allowed, retryAfter, err)
	}
}
