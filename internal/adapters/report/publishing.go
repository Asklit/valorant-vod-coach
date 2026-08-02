package report

import (
	"context"
	"errors"
	"fmt"
	"mime"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/asklit/valorant-vod-coach/internal/app"
	"github.com/asklit/valorant-vod-coach/internal/domain"
	"golang.org/x/sync/errgroup"
)

const ArtifactObjectPrefix = "artifacts"

type PublishingStore struct {
	Local        LocalStore
	Root         string
	Objects      app.BlobStore
	ObjectPrefix string
	Concurrency  int
}

func (s PublishingStore) SaveReport(ctx context.Context, report domain.AnalysisReport, overwrite bool) (app.SavedReport, error) {
	if s.Objects == nil {
		return app.SavedReport{}, errors.New("blob store is required")
	}
	saved, err := s.Local.SaveReport(ctx, report, overwrite)
	if err != nil {
		return app.SavedReport{}, err
	}
	files := referencedReportFiles(report, saved)
	group, publishCtx := errgroup.WithContext(ctx)
	concurrency := s.Concurrency
	if concurrency <= 0 || concurrency > 32 {
		concurrency = 8
	}
	group.SetLimit(concurrency)
	for _, localPath := range files {
		localPath := localPath
		group.Go(func() error {
			info, err := os.Stat(localPath)
			if errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("referenced artifact does not exist: %s", localPath)
			}
			if err != nil {
				return err
			}
			if !info.Mode().IsRegular() {
				return nil
			}
			key, err := ArtifactObjectKey(s.root(), s.objectPrefix(), localPath)
			if err != nil {
				return err
			}
			contentType := mime.TypeByExtension(strings.ToLower(filepath.Ext(localPath)))
			if contentType == "" {
				contentType = "application/octet-stream"
			}
			if err := s.Objects.UploadFile(publishCtx, key, localPath, contentType); err != nil {
				return fmt.Errorf("publish artifact %s: %w", localPath, err)
			}
			return nil
		})
	}
	if err := group.Wait(); err != nil {
		return app.SavedReport{}, err
	}
	return saved, nil
}

func ArtifactObjectKey(root string, prefix string, localPath string) (string, error) {
	rootAbs, err := filepath.Abs(strings.TrimSpace(root))
	if err != nil {
		return "", err
	}
	pathAbs, err := filepath.Abs(strings.TrimSpace(localPath))
	if err != nil {
		return "", err
	}
	relative, err := filepath.Rel(rootAbs, pathAbs)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
		return "", errors.New("artifact path is outside its workspace root")
	}
	prefix = strings.Trim(strings.TrimSpace(prefix), "/")
	if prefix == "" {
		prefix = ArtifactObjectPrefix
	}
	return path.Join(prefix, filepath.ToSlash(relative)), nil
}

func referencedReportFiles(report domain.AnalysisReport, saved app.SavedReport) []string {
	unique := map[string]struct{}{}
	add := func(value string) {
		if value = strings.TrimSpace(value); value != "" {
			unique[value] = struct{}{}
		}
	}
	add(saved.JSONPath)
	add(saved.MarkdownPath)
	add(report.Sample.ManifestPath)
	add(report.Sample.ContactSheetPath)
	for _, frame := range report.Sample.Frames {
		add(frame.Path)
	}
	for _, artifact := range report.Artifacts {
		add(artifact.Path)
	}
	for _, finding := range report.Findings {
		addEvidenceFiles(add, finding.Evidence)
	}
	if report.Gameplay != nil {
		for _, observation := range report.Gameplay.FrameObservations {
			add(observation.Path)
		}
		for _, event := range report.Gameplay.GameplayEvents {
			addEvidenceFiles(add, event.Evidence)
		}
		for _, window := range report.Gameplay.ReviewWindows {
			add(window.ClipPath)
			addEvidenceFiles(add, window.Evidence)
		}
		for _, task := range report.Gameplay.ModelReviewTasks {
			add(task.ClipPath)
			addEvidenceFiles(add, task.Evidence)
		}
	}
	files := make([]string, 0, len(unique))
	for value := range unique {
		files = append(files, value)
	}
	sort.Strings(files)
	return files
}

func addEvidenceFiles(add func(string), evidence []domain.EvidenceRef) {
	for _, item := range evidence {
		add(item.Path)
	}
}

func (s PublishingStore) root() string {
	if strings.TrimSpace(s.Root) != "" {
		return s.Root
	}
	return s.Local.ProcessedRoot
}

func (s PublishingStore) objectPrefix() string {
	if strings.TrimSpace(s.ObjectPrefix) != "" {
		return s.ObjectPrefix
	}
	return ArtifactObjectPrefix
}

var _ app.ReportStore = PublishingStore{}
