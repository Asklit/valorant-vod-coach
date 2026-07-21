package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/asklit/valorant-vod-coach/internal/adapters/dataset"
	"github.com/asklit/valorant-vod-coach/internal/adapters/media"
)

const (
	defaultManifest = "data/manifests/vods.tsv"
	defaultRawRoot  = "data/raw/youtube"
	defaultOutRoot  = "data/processed"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		printUsage(stderr)
		return 2
	}

	switch args[0] {
	case "dataset":
		return runDataset(args[1:], stdout, stderr)
	case "video":
		return runVideo(args[1:], stdout, stderr)
	case "help", "-h", "--help":
		printUsage(stdout)
		return 0
	default:
		fmt.Fprintf(stderr, "unknown command %q\n\n", args[0])
		printUsage(stderr)
		return 2
	}
}

func runVideo(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		printVideoUsage(stderr)
		return 2
	}

	switch args[0] {
	case "probe":
		return runVideoProbe(args[1:], stdout, stderr)
	case "sample":
		return runVideoSample(args[1:], stdout, stderr)
	case "help", "-h", "--help":
		printVideoUsage(stdout)
		return 0
	default:
		fmt.Fprintf(stderr, "unknown video command %q\n\n", args[0])
		printVideoUsage(stderr)
		return 2
	}
}

func runVideoProbe(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("vodctl video probe", flag.ContinueOnError)
	fs.SetOutput(stderr)
	manifestPath := fs.String("manifest", defaultManifest, "path to TSV manifest")
	rawRoot := fs.String("raw-root", defaultRawRoot, "root directory for downloaded videos")
	outRoot := fs.String("out-root", defaultOutRoot, "root directory for processed artifacts")
	ffprobePath := fs.String("ffprobe", "ffprobe", "ffprobe executable path")
	vodLabel := fs.String("vod", "", "manifest VOD label")
	printJSON := fs.Bool("print-json", false, "print raw ffprobe JSON to stdout")
	if ok, code := parseFlags(fs, args); !ok {
		return code
	}

	if strings.TrimSpace(*vodLabel) == "" {
		fmt.Fprintln(stderr, "--vod is required")
		return 2
	}

	vod, videoPath, err := loadVODAndVideoPath(*manifestPath, *rawRoot, *vodLabel)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	probe, err := media.RunProbe(ctx, *ffprobePath, videoPath)
	if err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return 1
	}

	artifactPath, err := media.WriteProbeArtifact(*outRoot, mediaVODInfo(vod), probe.Raw)
	if err != nil {
		fmt.Fprintf(stderr, "write probe artifact: %v\n", err)
		return 1
	}

	if *printJSON {
		_, _ = stdout.Write(probe.Raw)
		if len(probe.Raw) == 0 || probe.Raw[len(probe.Raw)-1] != '\n' {
			fmt.Fprintln(stdout)
		}
		return 0
	}

	videoStream, _ := media.VideoStream(probe.Metadata)
	audioStream, hasAudio := media.AudioStream(probe.Metadata)
	duration, hasDuration := media.Duration(probe.Metadata)
	size, hasSize := media.SizeBytes(probe.Metadata)

	durationText := "unknown"
	if hasDuration {
		durationText = duration.Round(time.Millisecond).String()
	}

	sizeText := "unknown"
	if hasSize {
		sizeText = formatBytes(size)
	}

	audioCodec := "none"
	if hasAudio {
		audioCodec = audioStream.CodecName
	}

	table := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(table, "LABEL\tDURATION\tVIDEO\tFPS\tAUDIO\tSIZE\tPROBE_JSON")
	fmt.Fprintf(table, "%s\t%s\t%s %s\t%s\t%s\t%s\t%s\n",
		vod.Label,
		durationText,
		videoStream.CodecName,
		media.Resolution(videoStream),
		media.FrameRate(videoStream),
		audioCodec,
		sizeText,
		artifactPath,
	)
	table.Flush()
	return 0
}

func runVideoSample(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("vodctl video sample", flag.ContinueOnError)
	fs.SetOutput(stderr)
	manifestPath := fs.String("manifest", defaultManifest, "path to TSV manifest")
	rawRoot := fs.String("raw-root", defaultRawRoot, "root directory for downloaded videos")
	outRoot := fs.String("out-root", defaultOutRoot, "root directory for processed artifacts")
	ffmpegPath := fs.String("ffmpeg", "ffmpeg", "ffmpeg executable path")
	vodLabel := fs.String("vod", "", "manifest VOD label")
	fps := fs.String("fps", "1", "frame sampling FPS")
	startRaw := fs.String("start", "0s", "start offset, for example 30s or 2m")
	durationRaw := fs.String("duration", "60s", "sample duration; use 0 for full input")
	sampleName := fs.String("name", "", "sample artifact name")
	imageQuality := fs.Int("image-quality", 3, "ffmpeg JPEG quality, lower is better")
	force := fs.Bool("force", false, "overwrite existing sample output")
	timeoutRaw := fs.String("timeout", "10m", "ffmpeg command timeout")
	if ok, code := parseFlags(fs, args); !ok {
		return code
	}

	if strings.TrimSpace(*vodLabel) == "" {
		fmt.Fprintln(stderr, "--vod is required")
		return 2
	}

	start, err := parseDurationArg("--start", *startRaw)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	if start < 0 {
		fmt.Fprintln(stderr, "--start must be non-negative")
		return 2
	}

	duration, err := parseDurationArg("--duration", *durationRaw)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	if duration < 0 {
		fmt.Fprintln(stderr, "--duration must be non-negative")
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

	vod, videoPath, err := loadVODAndVideoPath(*manifestPath, *rawRoot, *vodLabel)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}

	name := strings.TrimSpace(*sampleName)
	if name == "" {
		name = media.DefaultSampleName(*fps, start, duration)
	}
	name = media.SafeArtifactName(name)

	outputDir := media.SampleOutputDir(*outRoot, vod.Label, name)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	result, err := media.RunSample(ctx, media.SampleOptions{
		FFmpegPath:   *ffmpegPath,
		InputPath:    videoPath,
		OutputDir:    outputDir,
		FPS:          *fps,
		Start:        start,
		Duration:     duration,
		ImageQuality: *imageQuality,
		Overwrite:    *force,
	})
	if err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return 1
	}

	manifest, err := media.WriteFramesManifest(mediaVODInfo(vod), name, result)
	if err != nil {
		fmt.Fprintf(stderr, "write frames manifest: %v\n", err)
		return 1
	}

	durationText := duration.String()
	if duration == 0 {
		durationText = "full"
	}

	table := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(table, "LABEL\tSAMPLE\tFPS\tSTART\tDURATION\tFRAMES\tFRAMES_JSON")
	fmt.Fprintf(table, "%s\t%s\t%s\t%s\t%s\t%d\t%s\n",
		vod.Label,
		name,
		*fps,
		start.String(),
		durationText,
		result.FrameCount,
		manifest,
	)
	table.Flush()
	return 0
}

func runDataset(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		printDatasetUsage(stderr)
		return 2
	}

	switch args[0] {
	case "validate":
		return runDatasetValidate(args[1:], stdout, stderr)
	case "list":
		return runDatasetList(args[1:], stdout, stderr)
	case "status":
		return runDatasetStatus(args[1:], stdout, stderr)
	case "help", "-h", "--help":
		printDatasetUsage(stdout)
		return 0
	default:
		fmt.Fprintf(stderr, "unknown dataset command %q\n\n", args[0])
		printDatasetUsage(stderr)
		return 2
	}
}

func runDatasetValidate(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("vodctl dataset validate", flag.ContinueOnError)
	fs.SetOutput(stderr)
	manifestPath := fs.String("manifest", defaultManifest, "path to TSV manifest")
	if ok, code := parseFlags(fs, args); !ok {
		return code
	}

	vods, err := dataset.LoadManifest(*manifestPath)
	if err != nil {
		fmt.Fprintf(stderr, "load manifest: %v\n", err)
		return 1
	}

	issues := dataset.Validate(vods)
	for _, issue := range issues {
		fmt.Fprintf(stdout, "%s\tline=%d\tfield=%s\t%s\n", issue.Severity, issue.Line, issue.Field, issue.Message)
	}

	if dataset.HasErrors(issues) {
		fmt.Fprintf(stderr, "manifest invalid: %d records, %d enabled\n", len(vods), dataset.CountEnabled(vods))
		return 1
	}

	fmt.Fprintf(stdout, "manifest valid: %d records, %d enabled\n", len(vods), dataset.CountEnabled(vods))
	return 0
}

func runDatasetList(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("vodctl dataset list", flag.ContinueOnError)
	fs.SetOutput(stderr)
	manifestPath := fs.String("manifest", defaultManifest, "path to TSV manifest")
	rank := fs.String("rank", "", "rank filter")
	enabledOnly := fs.Bool("enabled-only", true, "show only enabled VODs")
	if ok, code := parseFlags(fs, args); !ok {
		return code
	}

	vods, err := loadFilteredManifest(*manifestPath, *rank, *enabledOnly)
	if err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return 1
	}

	table := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(table, "RANK\tLABEL\tVIDEO_ID\tDURATION\tTITLE")
	for _, vod := range vods {
		fmt.Fprintf(table, "%s\t%s\t%s\t%s\t%s\n", vod.Rank, vod.Label, vod.VideoID, vod.DurationRaw, vod.Title)
	}
	table.Flush()
	return 0
}

func runDatasetStatus(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("vodctl dataset status", flag.ContinueOnError)
	fs.SetOutput(stderr)
	manifestPath := fs.String("manifest", defaultManifest, "path to TSV manifest")
	rawRoot := fs.String("raw-root", defaultRawRoot, "root directory for downloaded videos")
	rank := fs.String("rank", "", "rank filter")
	enabledOnly := fs.Bool("enabled-only", true, "show only enabled VODs")
	if ok, code := parseFlags(fs, args); !ok {
		return code
	}

	vods, err := loadFilteredManifest(*manifestPath, *rank, *enabledOnly)
	if err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return 1
	}

	assets := dataset.ScanLocalAssets(*rawRoot, vods)
	table := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(table, "RANK\tLABEL\tSTATUS\tSIZE\tPATH")
	for _, asset := range assets {
		fmt.Fprintf(table, "%s\t%s\t%s\t%s\t%s\n", asset.VOD.Rank, asset.VOD.Label, asset.Status, formatBytes(asset.SizeBytes), asset.Path)
	}
	table.Flush()
	return 0
}

func loadFilteredManifest(path, rankRaw string, enabledOnly bool) ([]dataset.VOD, error) {
	vods, err := dataset.LoadManifest(path)
	if err != nil {
		return nil, fmt.Errorf("load manifest: %w", err)
	}

	rank := dataset.Rank(strings.TrimSpace(rankRaw))
	if rank != "" && !dataset.IsValidRank(rank) {
		return nil, fmt.Errorf("unknown rank %q", rank)
	}

	return dataset.Filter(vods, rank, enabledOnly), nil
}

func loadVODAndVideoPath(manifestPath, rawRoot, label string) (dataset.VOD, string, error) {
	vods, err := dataset.LoadManifest(manifestPath)
	if err != nil {
		return dataset.VOD{}, "", fmt.Errorf("load manifest: %w", err)
	}

	vod, ok := dataset.FindByLabel(vods, strings.TrimSpace(label))
	if !ok {
		return dataset.VOD{}, "", fmt.Errorf("unknown VOD label %q", label)
	}

	videoPath, _, ok := dataset.FindLocalVideo(rawRoot, vod)
	if !ok {
		return dataset.VOD{}, "", fmt.Errorf("video file not found: %s", videoPath)
	}

	return vod, videoPath, nil
}

func mediaVODInfo(vod dataset.VOD) media.VODInfo {
	return media.VODInfo{
		Label:   vod.Label,
		VideoID: vod.VideoID,
		Rank:    string(vod.Rank),
	}
}

func parseFlags(fs *flag.FlagSet, args []string) (bool, int) {
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return false, 0
		}
		return false, 2
	}
	return true, 0
}

func parseDurationArg(name, value string) (time.Duration, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, fmt.Errorf("%s is required", name)
	}
	if isDigits(value) {
		value += "s"
	}

	duration, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("invalid %s %q: %w", name, value, err)
	}
	return duration, nil
}

func isDigits(value string) bool {
	if value == "" {
		return false
	}
	for _, char := range value {
		if char < '0' || char > '9' {
			return false
		}
	}
	return true
}

func formatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}

	value := float64(bytes)
	for _, suffix := range []string{"KiB", "MiB", "GiB", "TiB"} {
		value = value / unit
		if value < unit {
			return fmt.Sprintf("%.1f %s", value, suffix)
		}
	}

	return fmt.Sprintf("%.1f PiB", value/unit)
}

func printUsage(w io.Writer) {
	fmt.Fprintln(w, `Usage:
  vodctl dataset <command> [options]
  vodctl video <command> [options]

Commands:
  dataset validate   Validate manifest structure and metadata
  dataset list       List VODs from the manifest
  dataset status     Show local download status
  video probe        Probe a downloaded video through ffprobe
  video sample       Extract sampled frames through ffmpeg`)
}

func printDatasetUsage(w io.Writer) {
	fmt.Fprintln(w, `Usage:
  vodctl dataset validate [--manifest path]
  vodctl dataset list [--manifest path] [--rank rank] [--enabled-only=false]
  vodctl dataset status [--manifest path] [--raw-root path] [--rank rank] [--enabled-only=false]`)
}

func printVideoUsage(w io.Writer) {
	fmt.Fprintln(w, `Usage:
  vodctl video probe --vod label [--manifest path] [--raw-root path] [--out-root path] [--ffprobe path] [--print-json]
  vodctl video sample --vod label [--manifest path] [--raw-root path] [--out-root path] [--ffmpeg path] [--fps n] [--start duration] [--duration duration] [--name name] [--force]`)
}
