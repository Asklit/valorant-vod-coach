package main

import (
	"context"
	"flag"
	"log"
	"os"
	"time"

	"github.com/asklit/valorant-vod-coach/internal/adapters/localanalysis"
	"github.com/asklit/valorant-vod-coach/internal/adapters/postgres"
	"github.com/asklit/valorant-vod-coach/internal/adapters/redislock"
	"github.com/asklit/valorant-vod-coach/internal/adapters/temporalworkflow"
	"github.com/asklit/valorant-vod-coach/internal/platform/observability"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
)

func main() {
	databaseURL := flag.String("database-url", os.Getenv("DATABASE_URL"), "PostgreSQL connection URL")
	redisURL := flag.String("redis-url", os.Getenv("REDIS_URL"), "optional Redis URL for distributed analysis locks")
	temporalAddress := flag.String("temporal-address", envString("TEMPORAL_ADDRESS", client.DefaultHostPort), "Temporal frontend address")
	temporalNamespace := flag.String("temporal-namespace", envString("TEMPORAL_NAMESPACE", client.DefaultNamespace), "Temporal namespace")
	taskQueue := flag.String("task-queue", envString("TEMPORAL_TASK_QUEUE", temporalworkflow.DefaultTaskQueue), "Temporal analysis task queue")
	manifestPath := flag.String("manifest", "data/manifests/vods.tsv", "path to TSV manifest")
	rawRoot := flag.String("raw-root", "data/raw/youtube", "root directory for downloaded videos")
	uploadRoot := flag.String("upload-root", "data/raw/uploads", "root directory for user-uploaded videos")
	processedRoot := flag.String("processed-root", "data/processed", "root directory for generated artifacts")
	ffprobePath := flag.String("ffprobe", "ffprobe", "ffprobe executable path")
	ffmpegPath := flag.String("ffmpeg", "ffmpeg", "ffmpeg executable path")
	tesseractPath := flag.String("tesseract", "tesseract", "Tesseract executable path")
	visionURL := flag.String("vision-url", os.Getenv("VISION_SERVICE_URL"), "optional vision-service base URL")
	postgresMigrationsDir := flag.String("postgres-migrations-dir", "deployments/migrations/postgres", "directory containing PostgreSQL migrations")
	migratePostgres := flag.Bool("migrate-postgres", true, "apply pending PostgreSQL migrations at startup")
	flag.Parse()

	if *databaseURL == "" {
		log.Fatal("DATABASE_URL or --database-url is required")
	}

	obs, err := observability.Setup(context.Background(), observability.Config{ServiceName: "vod-worker"}, os.Stderr)
	if err != nil {
		log.Fatalf("setup observability: %v", err)
	}
	defer observability.Shutdown(context.Background(), obs.Shutdown, obs.Logger)

	db, err := postgres.Open(context.Background(), *databaseURL)
	if err != nil {
		log.Fatalf("open PostgreSQL: %v", err)
	}
	defer db.Close()
	if *migratePostgres {
		if _, err := postgres.ApplyMigrations(context.Background(), db, *postgresMigrationsDir); err != nil {
			log.Fatalf("apply PostgreSQL migrations: %v", err)
		}
	}
	store := postgres.Store{DB: db, Producer: "vod-worker"}

	var locks *redislock.Manager
	if *redisURL != "" {
		manager, err := redislock.NewManager(*redisURL)
		if err != nil {
			log.Fatalf("configure Redis locks: %v", err)
		}
		defer manager.Close()
		locks = &manager
	}

	temporalClient, err := client.Dial(client.Options{
		HostPort:  *temporalAddress,
		Namespace: *temporalNamespace,
	})
	if err != nil {
		log.Fatalf("connect to Temporal: %v", err)
	}
	defer temporalClient.Close()

	executor := localanalysis.Service{Config: localanalysis.Config{
		ManifestPath:  *manifestPath,
		RawRoot:       *rawRoot,
		UploadRoot:    *uploadRoot,
		ProcessedRoot: *processedRoot,
		FFprobePath:   *ffprobePath,
		FFmpegPath:    *ffmpegPath,
		TesseractPath: *tesseractPath,
		VisionURL:     *visionURL,
		UploadCatalog: store,
		Catalog:       store,
		Locks:         locks,
	}}
	activities := temporalworkflow.Activities{Executor: executor, Jobs: store}
	temporalWorker := worker.New(temporalClient, *taskQueue, worker.Options{
		WorkerStopTimeout: 2 * time.Minute,
	})
	temporalworkflow.Register(temporalWorker, activities)

	obs.Logger.Info("vod-worker started", "task_queue", *taskQueue, "namespace", *temporalNamespace)
	if err := temporalWorker.Run(worker.InterruptCh()); err != nil {
		log.Fatalf("run Temporal worker: %v", err)
	}
}

func envString(name string, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
