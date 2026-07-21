package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/asklit/valorant-vod-coach/internal/adapters/webapi"
)

func main() {
	manifestPath := flag.String("manifest", "data/manifests/vods.tsv", "path to TSV manifest")
	rawRoot := flag.String("raw-root", "data/raw/youtube", "root directory for downloaded videos")
	processedRoot := flag.String("processed-root", "data/processed", "root directory for generated artifacts")
	ffprobePath := flag.String("ffprobe", "ffprobe", "ffprobe executable path")
	ffmpegPath := flag.String("ffmpeg", "ffmpeg", "ffmpeg executable path")
	visionURL := flag.String("vision-url", os.Getenv("VISION_SERVICE_URL"), "optional vision-service base URL; can also be set through VISION_SERVICE_URL")
	staticDir := flag.String("static-dir", "", "optional built frontend directory")
	addr := flag.String("addr", webapi.AddrFromEnv(8080), "HTTP listen address")
	flag.Parse()

	server := webapi.NewServer(webapi.Config{
		ManifestPath:  *manifestPath,
		RawRoot:       *rawRoot,
		ProcessedRoot: *processedRoot,
		FFprobePath:   *ffprobePath,
		FFmpegPath:    *ffmpegPath,
		VisionURL:     *visionURL,
		StaticDir:     *staticDir,
	})

	fmt.Fprintf(os.Stdout, "vod-web listening on http://localhost%s\n", *addr)
	if err := http.ListenAndServe(*addr, server); err != nil {
		log.Fatal(err)
	}
}
