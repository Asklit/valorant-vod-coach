package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/asklit/valorant-vod-coach/internal/adapters/postgres"
	"github.com/asklit/valorant-vod-coach/internal/adapters/redislock"
	"github.com/asklit/valorant-vod-coach/internal/adapters/redisrate"
	"github.com/asklit/valorant-vod-coach/internal/adapters/redissession"
	"github.com/asklit/valorant-vod-coach/internal/adapters/webapi"
	"github.com/asklit/valorant-vod-coach/internal/app"
	"github.com/asklit/valorant-vod-coach/internal/platform/observability"
)

func main() {
	manifestPath := flag.String("manifest", "data/manifests/vods.tsv", "path to TSV manifest")
	rawRoot := flag.String("raw-root", "data/raw/youtube", "root directory for downloaded videos")
	uploadRoot := flag.String("upload-root", "data/raw/uploads", "root directory for user-uploaded videos")
	maxUploadBytes := flag.Int64("max-upload-bytes", 6<<30, "maximum uploaded video size in bytes")
	processedRoot := flag.String("processed-root", "data/processed", "root directory for generated artifacts")
	ffprobePath := flag.String("ffprobe", "ffprobe", "ffprobe executable path")
	ffmpegPath := flag.String("ffmpeg", "ffmpeg", "ffmpeg executable path")
	visionURL := flag.String("vision-url", os.Getenv("VISION_SERVICE_URL"), "optional vision-service base URL; can also be set through VISION_SERVICE_URL")
	databaseURL := flag.String("database-url", os.Getenv("DATABASE_URL"), "optional Postgres URL for report metadata and outbox persistence")
	postgresMigrationsDir := flag.String("postgres-migrations-dir", "deployments/migrations/postgres", "directory containing PostgreSQL migrations")
	migratePostgres := flag.Bool("migrate-postgres", true, "apply pending PostgreSQL migrations at startup")
	redisURL := flag.String("redis-url", os.Getenv("REDIS_URL"), "optional Redis URL for analysis locks")
	bootstrapAdminToken := flag.String("bootstrap-admin-token", os.Getenv("VODCOACH_BOOTSTRAP_TOKEN"), "one-time token required to create the first administrator")
	staticDir := flag.String("static-dir", "", "optional built frontend directory")
	addr := flag.String("addr", webapi.AddrFromEnv(8080), "HTTP listen address")
	flag.Parse()

	obs, err := observability.Setup(context.Background(), observability.Config{ServiceName: "vod-web"}, os.Stderr)
	if err != nil {
		log.Fatalf("setup observability: %v", err)
	}
	defer observability.Shutdown(context.Background(), obs.Shutdown, obs.Logger)

	var catalog app.AnalysisCatalog
	var reportCatalog app.ReportCatalog
	var uploadCatalog app.UploadCatalog
	var authenticator app.Authenticator
	var jobStore app.AnalysisJobStore
	var userDataStore app.UserDataStore
	dependencies := map[string]app.HealthChecker{}
	if *databaseURL != "" {
		db, err := postgres.Open(context.Background(), *databaseURL)
		if err != nil {
			log.Fatalf("open postgres: %v", err)
		}
		defer db.Close()
		if *migratePostgres {
			applied, err := postgres.ApplyMigrations(context.Background(), db, *postgresMigrationsDir)
			if err != nil {
				log.Fatalf("apply PostgreSQL migrations: %v", err)
			}
			obs.Logger.Info("PostgreSQL migrations checked", "applied", len(applied))
		}
		store := postgres.Store{DB: db, Producer: "vod-web"}
		catalog = store
		reportCatalog = store
		uploadCatalog = store
		authenticator = store
		jobStore = store
		userDataStore = store
		dependencies["postgres"] = store
	}
	var locks app.LockManager
	var sessions app.AuthSessionStore
	var authRateLimiter app.RateLimiter
	if *redisURL != "" {
		manager, err := redislock.NewManager(*redisURL)
		if err != nil {
			log.Fatalf("configure redis locks: %v", err)
		}
		defer manager.Close()
		locks = manager
		sessionStore, err := redissession.New(*redisURL)
		if err != nil {
			log.Fatalf("configure Redis sessions: %v", err)
		}
		defer sessionStore.Close()
		sessions = sessionStore
		authRateLimiter = redisrate.Limiter{Client: sessionStore.Client}
		dependencies["redis"] = sessionStore
	}

	server := webapi.NewServer(webapi.Config{
		ManifestPath:        *manifestPath,
		RawRoot:             *rawRoot,
		UploadRoot:          *uploadRoot,
		MaxUploadBytes:      *maxUploadBytes,
		ProcessedRoot:       *processedRoot,
		FFprobePath:         *ffprobePath,
		FFmpegPath:          *ffmpegPath,
		VisionURL:           *visionURL,
		BootstrapAdminToken: *bootstrapAdminToken,
		StaticDir:           *staticDir,
		Catalog:             catalog,
		ReportCatalog:       reportCatalog,
		UploadCatalog:       uploadCatalog,
		UserDataStore:       userDataStore,
		Authenticator:       authenticator,
		SessionStore:        sessions,
		AuthRateLimiter:     authRateLimiter,
		JobStore:            jobStore,
		Dependencies:        dependencies,
		Locks:               locks,
		Logger:              obs.Logger,
		Tracer:              obs.Tracer,
	})

	obs.Logger.Info("vod-web listening", "addr", *addr, "static_dir", *staticDir, "database_enabled", *databaseURL != "", "redis_locks_enabled", *redisURL != "", "vision_configured", *visionURL != "")
	fmt.Fprintf(os.Stdout, "vod-web listening on http://localhost%s\n", *addr)
	if err := http.ListenAndServe(*addr, server); err != nil {
		log.Fatal(err)
	}
}
