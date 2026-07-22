package app

import (
	"context"
	"fmt"
	"strings"
	"time"
)

const defaultAnalysisLockTTL = 2 * time.Hour

type LockManager interface {
	Acquire(ctx context.Context, key string, ttl time.Duration) (Lock, error)
}

type Lock interface {
	Release(ctx context.Context) error
}

type LockAlreadyHeldError struct {
	Key string
}

func (e LockAlreadyHeldError) Error() string {
	return fmt.Sprintf("analysis lock already held: %s", e.Key)
}

func analysisLockKey(vodLabel string) string {
	cleaned := strings.TrimSpace(strings.ToLower(vodLabel))
	if cleaned == "" {
		cleaned = "unknown"
	}
	return "analysis:vod:" + cleaned
}
