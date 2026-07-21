package dataset

import (
	"os"
	"path/filepath"
)

var VideoExtensions = []string{".mp4", ".mkv", ".webm", ".mov"}

type LocalStatus string

const (
	LocalStatusMissing    LocalStatus = "missing"
	LocalStatusDownloaded LocalStatus = "downloaded"
)

type LocalAsset struct {
	VOD       VOD
	Status    LocalStatus
	Path      string
	SizeBytes int64
}

func ScanLocalAssets(rawRoot string, vods []VOD) []LocalAsset {
	assets := make([]LocalAsset, 0, len(vods))
	for _, vod := range vods {
		path, size, ok := FindLocalVideo(rawRoot, vod)
		status := LocalStatusMissing
		if ok {
			status = LocalStatusDownloaded
		}

		assets = append(assets, LocalAsset{
			VOD:       vod,
			Status:    status,
			Path:      path,
			SizeBytes: size,
		})
	}
	return assets
}

func FindLocalVideo(rawRoot string, vod VOD) (string, int64, bool) {
	for _, ext := range VideoExtensions {
		path := VideoPath(rawRoot, vod, ext)
		info, err := os.Stat(path)
		if err == nil && !info.IsDir() {
			return path, info.Size(), true
		}
	}

	return filepath.Join(rawRoot, string(vod.Rank), VideoFilename(vod, ".mp4")), 0, false
}
