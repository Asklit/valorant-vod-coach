package redisrate

import "testing"

func TestRedisIntAcceptsDriverRepresentations(t *testing.T) {
	for _, value := range []any{int64(42), "42", []byte("42")} {
		parsed, err := redisInt(value)
		if err != nil || parsed != 42 {
			t.Fatalf("parse %T: value=%d err=%v", value, parsed, err)
		}
	}
	if _, err := redisInt(true); err == nil {
		t.Fatal("unexpected type must be rejected")
	}
}
