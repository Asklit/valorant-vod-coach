package redissession

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/asklit/valorant-vod-coach/internal/app"
)

func TestRedisSessionLifecycleIntegration(t *testing.T) {
	redisURL := strings.TrimSpace(os.Getenv("TEST_REDIS_URL"))
	if redisURL == "" {
		t.Skip("TEST_REDIS_URL is not set")
	}
	store, err := New(redisURL)
	if err != nil {
		t.Fatalf("configure Redis: %v", err)
	}
	defer store.Close()
	store.Prefix = "vodcoach:test:session:" + time.Now().UTC().Format("20060102150405.000000000") + ":"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	session, err := store.Create(ctx, app.PublicAuthUser{ID: "user_test", Email: "player@example.com", Role: app.AuthRoleUser}, time.Minute)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	defer store.Delete(context.Background(), session.Token)
	loaded, found, err := store.Get(ctx, session.Token)
	if err != nil || !found || loaded.CSRFToken != session.CSRFToken || loaded.User.ID != "user_test" {
		t.Fatalf("load session: %+v found=%v err=%v", loaded, found, err)
	}
	if err := store.Delete(ctx, session.Token); err != nil {
		t.Fatalf("delete session: %v", err)
	}
	if _, found, err := store.Get(ctx, session.Token); err != nil || found {
		t.Fatalf("deleted session must be absent: found=%v err=%v", found, err)
	}
}
