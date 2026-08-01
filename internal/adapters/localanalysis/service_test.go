package localanalysis

import (
	"path/filepath"
	"testing"
	"time"
)

func TestProcessedRootForOwnerIsTenantScoped(t *testing.T) {
	root := t.TempDir()
	want := filepath.Join(root, "users", "user_123", "analyses")
	if got := ProcessedRootForOwner(root, "user_123"); got != want {
		t.Fatalf("ProcessedRootForOwner() = %q, want %q", got, want)
	}
	if got := ProcessedRootForOwner(root, "../other"); got != filepath.Join(root, "users", "invalid", "analyses") {
		t.Fatalf("unsafe owner root = %q", got)
	}
}

func TestAnalysisTimeoutsScaleForFullVOD(t *testing.T) {
	if got := OverallTimeout(180); got != 15*time.Minute {
		t.Fatalf("short overall timeout = %s", got)
	}
	if got := OverallTimeout(0); got != 50*time.Minute {
		t.Fatalf("full VOD overall timeout = %s", got)
	}
	if got := SampleTimeout(0); got != 45*time.Minute {
		t.Fatalf("full VOD sample timeout = %s", got)
	}
}
