package media

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type VODInfo struct {
	Label   string
	VideoID string
	Rank    string
}

type Metadata struct {
	Streams []Stream `json:"streams"`
	Format  Format   `json:"format"`
}

type Stream struct {
	Index        int    `json:"index"`
	CodecName    string `json:"codec_name"`
	CodecType    string `json:"codec_type"`
	Width        int    `json:"width,omitempty"`
	Height       int    `json:"height,omitempty"`
	AvgFrameRate string `json:"avg_frame_rate,omitempty"`
	Duration     string `json:"duration,omitempty"`
	BitRate      string `json:"bit_rate,omitempty"`
}

type Format struct {
	Filename       string `json:"filename"`
	NBStreams      int    `json:"nb_streams"`
	FormatName     string `json:"format_name"`
	FormatLongName string `json:"format_long_name"`
	StartTime      string `json:"start_time"`
	Duration       string `json:"duration"`
	Size           string `json:"size"`
	BitRate        string `json:"bit_rate"`
}

type Probe struct {
	Raw      []byte
	Metadata Metadata
}

func RunProbe(ctx context.Context, ffprobePath, inputPath string) (Probe, error) {
	raw, err := runFFProbe(ctx, ffprobePath, inputPath)
	if err != nil {
		return Probe{}, err
	}

	metadata, err := ParseMetadata(raw)
	if err != nil {
		return Probe{}, err
	}

	return Probe{
		Raw:      raw,
		Metadata: metadata,
	}, nil
}

func ParseMetadata(raw []byte) (Metadata, error) {
	var metadata Metadata
	decoder := json.NewDecoder(bytes.NewReader(raw))

	if err := decoder.Decode(&metadata); err != nil {
		return Metadata{}, fmt.Errorf("parse ffprobe JSON: %w", err)
	}

	return metadata, nil
}

func WriteProbeArtifact(processedRoot string, vod VODInfo, raw []byte) (string, error) {
	dir := filepath.Join(processedRoot, vod.Label)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}

	path := filepath.Join(dir, "probe.ffprobe.json")
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		return "", err
	}

	return path, nil
}

func VideoStream(metadata Metadata) (Stream, bool) {
	for _, stream := range metadata.Streams {
		if stream.CodecType == "video" {
			return stream, true
		}
	}
	return Stream{}, false
}

func AudioStream(metadata Metadata) (Stream, bool) {
	for _, stream := range metadata.Streams {
		if stream.CodecType == "audio" {
			return stream, true
		}
	}
	return Stream{}, false
}

func Duration(metadata Metadata) (time.Duration, bool) {
	if metadata.Format.Duration == "" {
		return 0, false
	}

	seconds, err := strconv.ParseFloat(metadata.Format.Duration, 64)
	if err != nil {
		return 0, false
	}

	return time.Duration(seconds * float64(time.Second)), true
}

func SizeBytes(metadata Metadata) (int64, bool) {
	if metadata.Format.Size == "" {
		return 0, false
	}

	size, err := strconv.ParseInt(metadata.Format.Size, 10, 64)
	if err != nil {
		return 0, false
	}

	return size, true
}

func Resolution(stream Stream) string {
	if stream.Width <= 0 || stream.Height <= 0 {
		return "unknown"
	}
	return fmt.Sprintf("%dx%d", stream.Width, stream.Height)
}

func FrameRate(stream Stream) string {
	if stream.AvgFrameRate == "" || stream.AvgFrameRate == "0/0" {
		return "unknown"
	}

	parts := strings.Split(stream.AvgFrameRate, "/")
	if len(parts) != 2 {
		return stream.AvgFrameRate
	}

	numerator, err := strconv.ParseFloat(parts[0], 64)
	if err != nil {
		return stream.AvgFrameRate
	}

	denominator, err := strconv.ParseFloat(parts[1], 64)
	if err != nil || denominator == 0 {
		return stream.AvgFrameRate
	}

	fps := numerator / denominator
	if math.Abs(fps-math.Round(fps)) < 0.01 {
		return fmt.Sprintf("%.0f fps", fps)
	}
	return fmt.Sprintf("%.2f fps", fps)
}

func runFFProbe(ctx context.Context, ffprobePath, inputPath string) ([]byte, error) {
	cmd := exec.CommandContext(
		ctx,
		ffprobePath,
		"-v", "error",
		"-print_format", "json",
		"-show_format",
		"-show_streams",
		inputPath,
	)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	raw, err := cmd.Output()
	if err != nil {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = err.Error()
		}
		return nil, fmt.Errorf("ffprobe %q: %s", inputPath, message)
	}

	return raw, nil
}
