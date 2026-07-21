package dataset

import (
	"context"
	"fmt"
	"strings"

	"github.com/asklit/valorant-vod-coach/internal/domain"
)

type LocalVODResolver struct {
	ManifestPath string
	RawRoot      string
}

func (r LocalVODResolver) ResolveVOD(ctx context.Context, label string) (domain.VOD, string, error) {
	if err := ctx.Err(); err != nil {
		return domain.VOD{}, "", err
	}

	vods, err := LoadManifest(r.ManifestPath)
	if err != nil {
		return domain.VOD{}, "", fmt.Errorf("load manifest: %w", err)
	}

	vod, ok := FindByLabel(vods, strings.TrimSpace(label))
	if !ok {
		return domain.VOD{}, "", fmt.Errorf("unknown VOD label %q", label)
	}

	videoPath, _, ok := FindLocalVideo(r.RawRoot, vod)
	if !ok {
		return domain.VOD{}, "", fmt.Errorf("video file not found: %s", videoPath)
	}

	return ToDomainVOD(vod), videoPath, nil
}

func ToDomainVOD(vod VOD) domain.VOD {
	return domain.VOD{
		Label:                   vod.Label,
		VideoID:                 vod.VideoID,
		Rank:                    domain.Rank(vod.Rank),
		SourceURL:               vod.URL,
		Title:                   vod.Title,
		Channel:                 vod.Channel,
		ManifestDurationSeconds: vod.Duration.Seconds(),
	}
}
