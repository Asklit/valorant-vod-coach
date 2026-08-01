package redissession

import (
	"strings"
	"testing"
)

func TestSessionKeyHashesBearerToken(t *testing.T) {
	store := Store{Prefix: "test:session:"}
	token := "top-secret-cookie-token"
	key := store.key(token)
	if strings.Contains(key, token) || !strings.HasPrefix(key, store.Prefix) {
		t.Fatalf("session key must be prefixed and hash the token: %q", key)
	}
	if key != store.key(token) || key == store.key(token+"-different") {
		t.Fatalf("session key hashing must be deterministic and collision-resistant")
	}
}
