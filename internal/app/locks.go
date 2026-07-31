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

func analysisLockKey(ownerID string, vodLabel string) string {
	owner := strings.TrimSpace(strings.ToLower(ownerID))
	if owner == "" {
		owner = "system"
	}
	cleaned := strings.TrimSpace(strings.ToLower(vodLabel))
	if cleaned == "" {
		cleaned = "unknown"
	}
	return "analysis:owner:" + owner + ":vod:" + cleaned
}
