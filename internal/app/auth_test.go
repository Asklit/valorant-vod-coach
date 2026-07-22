package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAuthStoreRegistersFirstUserAsAdmin(t *testing.T) {
	store := AuthStore{
		Path:       filepath.Join(t.TempDir(), "users.json"),
		Iterations: 4,
		Clock: func() time.Time {
			return time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
		},
	}

	admin, err := store.Register(context.Background(), AuthRegisterRequest{
		Email:       "Coach@Example.com",
		Password:    "secret-pass",
		DisplayName: "Coach",
	})
	if err != nil {
		t.Fatalf("register admin: %v", err)
	}
	if admin.Role != AuthRoleAdmin || admin.Email != "coach@example.com" {
		t.Fatalf("unexpected admin: %+v", admin)
	}

	user, err := store.Register(context.Background(), AuthRegisterRequest{
		Email:    "player@example.com",
		Password: "secret-pass",
	})
	if err != nil {
		t.Fatalf("register user: %v", err)
	}
	if user.Role != AuthRoleUser || user.DisplayName != "player" {
		t.Fatalf("unexpected user: %+v", user)
	}

	raw, err := os.ReadFile(store.Path)
	if err != nil {
		t.Fatalf("read users: %v", err)
	}
	if strings.Contains(string(raw), "secret-pass") {
		t.Fatalf("users file contains raw password:\n%s", raw)
	}
	if !strings.Contains(string(raw), "pbkdf2_sha256") {
		t.Fatalf("users file missing password hash:\n%s", raw)
	}
}

func TestAuthStoreAuthenticatesUser(t *testing.T) {
	store := AuthStore{Path: filepath.Join(t.TempDir(), "users.json"), Iterations: 4}
	registered, err := store.Register(context.Background(), AuthRegisterRequest{
		Email:    "player@example.com",
		Password: "secret-pass",
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	if _, err := store.Authenticate(context.Background(), AuthLoginRequest{Email: registered.Email, Password: "wrong-pass"}); err == nil {
		t.Fatalf("expected invalid password error")
	}
	authenticated, err := store.Authenticate(context.Background(), AuthLoginRequest{
		Email:    "PLAYER@example.com",
		Password: "secret-pass",
	})
	if err != nil {
		t.Fatalf("authenticate: %v", err)
	}
	if authenticated.ID != registered.ID || authenticated.LastLoginAt == nil {
		t.Fatalf("unexpected authenticated user: %+v", authenticated)
	}
}
