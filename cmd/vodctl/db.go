package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/asklit/valorant-vod-coach/internal/adapters/postgres"
)

const defaultPostgresMigrationsDir = "deployments/migrations/postgres"

func runDB(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		printDBUsage(stderr)
		return 2
	}

	switch args[0] {
	case "migrate":
		return runDBMigrate(args[1:], stdout, stderr)
	case "help", "-h", "--help":
		printDBUsage(stdout)
		return 0
	default:
		fmt.Fprintf(stderr, "unknown db command %q\n\n", args[0])
		printDBUsage(stderr)
		return 2
	}
}

func runDBMigrate(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("vodctl db migrate", flag.ContinueOnError)
	fs.SetOutput(stderr)
	databaseURL := fs.String("database-url", os.Getenv("DATABASE_URL"), "Postgres connection URL; can also be set through DATABASE_URL")
	migrationsDir := fs.String("migrations-dir", defaultPostgresMigrationsDir, "directory with PostgreSQL migration .sql files")
	timeoutRaw := fs.String("timeout", "30s", "migration timeout")
	if ok, code := parseFlags(fs, args); !ok {
		return code
	}
	if strings.TrimSpace(*databaseURL) == "" {
		fmt.Fprintln(stderr, "--database-url or DATABASE_URL is required")
		return 2
	}
	timeout, err := parseDurationArg("--timeout", *timeoutRaw)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	if timeout <= 0 {
		fmt.Fprintln(stderr, "--timeout must be positive")
		return 2
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	db, err := postgres.Open(ctx, *databaseURL)
	if err != nil {
		fmt.Fprintf(stderr, "open postgres: %v\n", err)
		return 1
	}
	defer db.Close()

	applied, err := postgres.ApplyMigrations(ctx, db, *migrationsDir)
	if err != nil {
		fmt.Fprintf(stderr, "apply migrations: %v\n", err)
		return 1
	}

	table := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(table, "STATUS\tVERSION\tNAME")
	if len(applied) == 0 {
		fmt.Fprintln(table, "up-to-date\t-\t-")
	} else {
		for _, migration := range applied {
			fmt.Fprintf(table, "applied\t%d\t%s\n", migration.Version, migration.Name)
		}
	}
	table.Flush()
	return 0
}

func printDBUsage(w io.Writer) {
	fmt.Fprintln(w, `Usage:
  vodctl db migrate [--database-url url] [--migrations-dir dir] [--timeout duration]

The db command applies PostgreSQL migrations used by the local MVP metadata store and outbox.`)
}
