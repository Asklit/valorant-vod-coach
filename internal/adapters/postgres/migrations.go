package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type Migration struct {
	Version int
	Name    string
	SQL     string
}

func LoadMigrations(dir string) ([]Migration, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	migrations := make([]Migration, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		version, err := migrationVersion(entry.Name())
		if err != nil {
			return nil, err
		}
		raw, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, err
		}
		migrations = append(migrations, Migration{
			Version: version,
			Name:    entry.Name(),
			SQL:     string(raw),
		})
	}
	sort.Slice(migrations, func(i, j int) bool {
		return migrations[i].Version < migrations[j].Version
	})
	return migrations, nil
}

func ApplyMigrations(ctx context.Context, db *sql.DB, dir string) ([]Migration, error) {
	migrations, err := LoadMigrations(dir)
	if err != nil {
		return nil, err
	}
	applied := make([]Migration, 0, len(migrations))
	for _, migration := range migrations {
		didApply, err := applyMigration(ctx, db, migration)
		if err != nil {
			return nil, err
		}
		if didApply {
			applied = append(applied, migration)
		}
	}
	return applied, nil
}

func applyMigration(ctx context.Context, db *sql.DB, migration Migration) (bool, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()

	var exists bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS (
SELECT 1 FROM information_schema.tables WHERE table_schema = 'public' AND table_name = 'schema_migrations'
)`).Scan(&exists); err != nil {
		return false, err
	}
	if exists {
		var alreadyApplied bool
		if err := tx.QueryRowContext(ctx, "SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = $1)", migration.Version).Scan(&alreadyApplied); err != nil {
			return false, err
		}
		if alreadyApplied {
			return false, tx.Commit()
		}
	}

	if _, err := tx.ExecContext(ctx, migration.SQL); err != nil {
		return false, fmt.Errorf("apply migration %s: %w", migration.Name, err)
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO schema_migrations (version, name) VALUES ($1, $2) ON CONFLICT (version) DO NOTHING", migration.Version, migration.Name); err != nil {
		return false, err
	}
	return true, tx.Commit()
}

func migrationVersion(name string) (int, error) {
	head, _, ok := strings.Cut(name, "_")
	if !ok {
		return 0, fmt.Errorf("migration %q must start with <version>_", name)
	}
	version, err := strconv.Atoi(head)
	if err != nil {
		return 0, fmt.Errorf("parse migration version %q: %w", name, err)
	}
	return version, nil
}
