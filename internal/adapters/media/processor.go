package media

import (
	"context"
	"time"

	"github.com/asklit/valorant-vod-coach/internal/app"
	"github.com/asklit/valorant-vod-coach/internal/domain"
)

type LocalProcessor struct {
	FFprobePath   string
	FFmpegPath    string
	ProcessedRoot string
	ProbeTimeout  time.Duration
	SampleTimeout time.Duration
}

func (p LocalProcessor) ProbeMedia(ctx context.Context, vod domain.VOD, videoPath string) (app.MediaProbeResult, error) {
	ffprobePath := p.FFprobePath
	if ffprobePath == "" {
		ffprobePath = "ffprobe"
	}

	runCtx := ctx
	cancel := func() {}
	if p.ProbeTimeout > 0 {
		runCtx, cancel = context.WithTimeout(ctx, p.ProbeTimeout)
	}
	defer cancel()

	probe, err := RunProbe(runCtx, ffprobePath, videoPath)
	if err != nil {
		return app.MediaProbeResult{}, err
	}

	artifactPath, err := WriteProbeArtifact(p.ProcessedRoot, mediaVODInfo(vod), probe.Raw)
	if err != nil {
		return app.MediaProbeResult{}, err
	}

	return app.MediaProbeResult{
		Summary: summarizeMetadata(probe.Metadata),
		Artifact: domain.Artifact{
			Type:   "media_probe",
			Format: "ffprobe_json",
			Path:   artifactPath,
		},
	}, nil
}

func (p LocalProcessor) SampleFrames(ctx context.Context, vod domain.VOD, videoPath string, request app.FrameSampleRequest) (app.FrameSampleResult, error) {
	ffmpegPath := p.FFmpegPath
	if ffmpegPath == "" {
		ffmpegPath = "ffmpeg"
	}
	sampleName := SafeArtifactName(request.Name)

	runCtx := ctx
	cancel := func() {}
	if p.SampleTimeout > 0 {
		runCtx, cancel = context.WithTimeout(ctx, p.SampleTimeout)
	}
	defer cancel()

	outputDir := SampleOutputDir(p.ProcessedRoot, vod.Label, sampleName)
	result, err := RunSample(runCtx, SampleOptions{
		FFmpegPath:   ffmpegPath,
		InputPath:    videoPath,
		OutputDir:    outputDir,
		FPS:          request.FPS,
		Start:        request.Start,
		Duration:     request.Duration,
		ImageQuality: request.ImageQuality,
		Overwrite:    request.Overwrite,
	})
	if err != nil {
		return app.FrameSampleResult{}, err
	}

	manifestPath, err := WriteFramesManifest(mediaVODInfo(vod), sampleName, result)
	if err != nil {
		return app.FrameSampleResult{}, err
	}

	return app.FrameSampleResult{
		Summary: domain.FrameSampleSummary{
			Name:            sampleName,
			OutputDir:       result.OutputDir,
			ManifestPath:    manifestPath,
			FPS:             result.FPS,
			FPSValue:        result.FPSValue,
			StartSeconds:    result.Start.Seconds(),
			DurationSeconds: result.Duration.Seconds(),
			FrameCount:      result.FrameCount,
			Frames:          summarizeFrames(result.Frames),
		},
		Artifact: domain.Artifact{
			Type:   "frame_sample",
			Format: "frames_manifest_json",
			Path:   manifestPath,
		},
	}, nil
}

func summarizeMetadata(metadata Metadata) domain.MediaSummary {
	summary := domain.MediaSummary{}
	if duration, ok := Duration(metadata); ok {
		summary.DurationSeconds = duration.Seconds()
		summary.HasDuration = true
	}
	if size, ok := SizeBytes(metadata); ok {
		summary.SizeBytes = size
		summary.HasSize = true
	}
	if stream, ok := VideoStream(metadata); ok {
		summary.VideoCodec = stream.CodecName
		summary.Width = stream.Width
		summary.Height = stream.Height
		summary.FrameRate = FrameRate(stream)
	}
	if stream, ok := AudioStream(metadata); ok {
		summary.AudioCodec = stream.CodecName
		summary.HasAudio = true
	}
	return summary
}

func summarizeFrames(frames []FrameArtifact) []domain.Frame {
	summary := make([]domain.Frame, 0, len(frames))
	for _, frame := range frames {
		summary = append(summary, domain.Frame{
			Index:            frame.Index,
			TimestampSeconds: frame.TimestampSeconds,
			Path:             frame.Path,
		})
	}
	return summary
}

func mediaVODInfo(vod domain.VOD) VODInfo {
	return VODInfo{
		Label:   vod.Label,
		VideoID: vod.VideoID,
		Rank:    string(vod.Rank),
	}
}
