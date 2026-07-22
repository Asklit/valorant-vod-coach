package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/asklit/valorant-vod-coach/internal/domain"
)

func TestManualCorrectionsAppendAndLoad(t *testing.T) {
	root := t.TempDir()
	loaded, saved, err := LoadManualCorrections(root, "diamond_example", "run_01")
	if err != nil {
		t.Fatalf("load missing corrections: %v", err)
	}
	if loaded.VODLabel != "diamond_example" || len(loaded.Corrections) != 0 {
		t.Fatalf("unexpected empty set: %+v", loaded)
	}
	if saved.JSONPath != filepath.Join(root, "diamond_example", "run_01", ManualCorrectionsJSONName) {
		t.Fatalf("unexpected corrections path: %s", saved.JSONPath)
	}

	now := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	timestamp := 42.5
	set, saved, err := AppendManualCorrection(context.Background(), root, "diamond_example", "run_01", domain.ManualCorrection{
		Type:             "false_detection",
		TargetID:         "event_001",
		Comment:          "This was a rotation, not combat.",
		TimestampSeconds: &timestamp,
	}, now)
	if err != nil {
		t.Fatalf("append correction: %v", err)
	}
	if len(set.Corrections) != 1 || set.Corrections[0].Status != "open" || set.Corrections[0].ID == "" {
		t.Fatalf("unexpected corrections set: %+v", set)
	}
	raw, err := os.ReadFile(saved.JSONPath)
	if err != nil {
		t.Fatalf("read corrections file: %v", err)
	}
	if !strings.Contains(string(raw), `"type": "false_detection"`) ||
		!strings.Contains(string(raw), `"timestamp_seconds": 42.5`) {
		t.Fatalf("unexpected corrections file:\n%s", raw)
	}

	loaded, _, err = LoadManualCorrections(root, "diamond_example", "run_01")
	if err != nil {
		t.Fatalf("reload corrections: %v", err)
	}
	if len(loaded.Corrections) != 1 || loaded.Corrections[0].TargetID != "event_001" {
		t.Fatalf("unexpected reloaded corrections: %+v", loaded)
	}
}

func TestManualCorrectionsRequireActionableContent(t *testing.T) {
	_, _, err := AppendManualCorrection(context.Background(), t.TempDir(), "diamond_example", "run_01", domain.ManualCorrection{
		Type: "map",
	}, time.Now())
	if err == nil || !strings.Contains(err.Error(), "correction value or comment is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}
