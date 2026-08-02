package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/asklit/valorant-vod-coach/internal/adapters/postgres"
	"github.com/asklit/valorant-vod-coach/internal/adapters/redislock"
	"github.com/asklit/valorant-vod-coach/internal/adapters/redisrate"
	"github.com/asklit/valorant-vod-coach/internal/adapters/redissession"
	"github.com/asklit/valorant-vod-coach/internal/adapters/s3store"
	"github.com/asklit/valorant-vod-coach/internal/adapters/temporalworkflow"
	"github.com/asklit/valorant-vod-coach/internal/adapters/webapi"
	"github.com/asklit/valorant-vod-coach/internal/app"
	"github.com/asklit/valorant-vod-coach/internal/platform/observability"
	"go.temporal.io/sdk/client"
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
	s3Endpoint := flag.String("s3-endpoint", os.Getenv("S3_ENDPOINT"), "optional S3-compatible endpoint")
	s3Region := flag.String("s3-region", envString("S3_REGION", "us-east-1"), "S3 region")
	s3Bucket := flag.String("s3-bucket", os.Getenv("S3_BUCKET"), "optional S3 bucket enabling durable VOD/artifact storage")
	s3Prefix := flag.String("s3-prefix", os.Getenv("S3_PREFIX"), "optional S3 key prefix")
	s3PathStyle := flag.Bool("s3-path-style", envBool("S3_USE_PATH_STYLE", os.Getenv("S3_ENDPOINT") != ""), "use path-style S3 addressing (required by local MinIO)")
	temporalAddress := flag.String("temporal-address", os.Getenv("TEMPORAL_ADDRESS"), "optional Temporal frontend address for durable analysis workflows")
	temporalNamespace := flag.String("temporal-namespace", envString("TEMPORAL_NAMESPACE", client.DefaultNamespace), "Temporal namespace")
	temporalTaskQueue := flag.String("temporal-task-queue", envString("TEMPORAL_TASK_QUEUE", temporalworkflow.DefaultTaskQueue), "Temporal analysis task queue")
	bootstrapAdminToken := flag.String("bootstrap-admin-token", os.Getenv("VODCOACH_BOOTSTRAP_TOKEN"), "one-time token required to create the first administrator")
	staticDir := flag.String("static-dir", "", "optional built frontend directory")
	addr := flag.String("addr", webapi.AddrFromEnv(8080), "HTTP listen address")
	flag.Parse()

	serviceCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	obs, err := observability.Setup(serviceCtx, observability.Config{ServiceName: "vod-web"}, os.Stderr)
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
	var objects app.BlobStore
	if *s3Bucket != "" {
		store, err := s3store.New(serviceCtx, s3store.Config{
			Endpoint:        *s3Endpoint,
			Region:          *s3Region,
			Bucket:          *s3Bucket,
			AccessKeyID:     os.Getenv("S3_ACCESS_KEY_ID"),
			SecretAccessKey: os.Getenv("S3_SECRET_ACCESS_KEY"),
			SessionToken:    os.Getenv("S3_SESSION_TOKEN"),
			Prefix:          *s3Prefix,
			UsePathStyle:    *s3PathStyle,
		})
		if err != nil {
			log.Fatalf("configure S3 object storage: %v", err)
		}
		objects = store
		dependencies["object_storage"] = store
	}
	if *databaseURL != "" {
		db, err := postgres.Open(serviceCtx, *databaseURL)
		if err != nil {
			log.Fatalf("open postgres: %v", err)
		}
		defer db.Close()
		if *migratePostgres {
			applied, err := postgres.ApplyMigrations(serviceCtx, db, *postgresMigrationsDir)
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
	var workflowLauncher app.AnalysisWorkflowLauncher
	if *temporalAddress != "" {
		if jobStore == nil {
			log.Fatal("Temporal workflows require DATABASE_URL for the durable job read model")
		}
		temporalClient, err := client.Dial(client.Options{
			HostPort:  *temporalAddress,
			Namespace: *temporalNamespace,
		})
		if err != nil {
			log.Fatalf("connect to Temporal: %v", err)
		}
		defer temporalClient.Close()
		workflowLauncher = temporalworkflow.Launcher{Client: temporalClient, TaskQueue: *temporalTaskQueue}
		dependencies["temporal"] = temporalworkflow.HealthChecker{Client: temporalClient}
	}
	if workflowLauncher != nil {
		dispatcher := temporalworkflow.Dispatcher{
			Launcher: workflowLauncher,
			Jobs:     jobStore,
			OnError: func(err error) {
				obs.Logger.Warn("reconcile queued workflows", "error", err)
			},
		}
		go dispatcher.Run(serviceCtx)
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
		Objects:             objects,
		WorkflowLauncher:    workflowLauncher,
		Logger:              obs.Logger,
		Tracer:              obs.Tracer,
	})

	obs.Logger.Info("vod-web listening", "addr", *addr, "static_dir", *staticDir, "database_enabled", *databaseURL != "", "redis_locks_enabled", *redisURL != "", "object_storage_enabled", objects != nil, "temporal_enabled", *temporalAddress != "", "vision_configured", *visionURL != "")
	fmt.Fprintf(os.Stdout, "vod-web listening on http://localhost%s\n", *addr)
	httpServer := &http.Server{
		Addr:              *addr,
		Handler:           server,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}
	serverErrors := make(chan error, 1)
	go func() {
		serverErrors <- httpServer.ListenAndServe()
	}()
	select {
	case err := <-serverErrors:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			obs.Logger.Error("vod-web stopped unexpectedly", "error", err)
		}
	case <-serviceCtx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			obs.Logger.Error("shutdown vod-web", "error", err)
		}
	}
}

func envString(name string, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func envBool(name string, fallback bool) bool {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}
