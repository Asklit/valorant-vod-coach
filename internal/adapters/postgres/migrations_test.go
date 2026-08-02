package postgres

import (
	"strings"
	"testing"
)

func TestLoadMigrations(t *testing.T) {
	migrations, err := LoadMigrations("../../../deployments/migrations/postgres")
	if err != nil {
		t.Fatalf("load migrations: %v", err)
	}
	if len(migrations) != 5 {
		t.Fatalf("expected five migrations, got %d", len(migrations))
	}
	if migrations[1].Version != 2 || !strings.Contains(migrations[1].SQL, "CREATE TABLE IF NOT EXISTS auth_users") ||
		!strings.Contains(migrations[1].SQL, "CREATE TABLE IF NOT EXISTS analysis_jobs") ||
		!strings.Contains(migrations[1].SQL, "idx_analysis_reports_owner_vod_run") {
		t.Fatalf("unexpected product persistence migration: %+v", migrations[1])
	}
	if migrations[0].Version != 1 || migrations[0].Name != "001_init.sql" {
		t.Fatalf("unexpected migration: %+v", migrations[0])
	}
	if migrations[2].Version != 3 || !strings.Contains(migrations[2].SQL, "progress_percent") {
		t.Fatalf("unexpected job progress migration: %+v", migrations[2])
	}
	if migrations[3].Version != 4 || !strings.Contains(migrations[3].SQL, "video_object_key") {
		t.Fatalf("unexpected object storage migration: %+v", migrations[3])
	}
	if migrations[4].Version != 5 || !strings.Contains(migrations[4].SQL, "trace_parent") {
		t.Fatalf("unexpected outbox trace migration: %+v", migrations[4])
	}
	for _, expected := range []string{
		"CREATE TABLE IF NOT EXISTS vods",
		"CREATE TABLE IF NOT EXISTS analysis_reports",
		"CREATE TABLE IF NOT EXISTS outbox_events",
		"idx_outbox_events_pending",
	} {
		if !strings.Contains(migrations[0].SQL, expected) {
			t.Fatalf("migration missing %q", expected)
		}
	}
}
