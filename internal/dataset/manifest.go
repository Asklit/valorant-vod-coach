package dataset

import (
	"encoding/csv"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const ManifestColumns = 10

var labelPattern = regexp.MustCompile(`^[a-z0-9_]+$`)

type Rank string

const (
	RankIron      Rank = "iron"
	RankBronze    Rank = "bronze"
	RankSilver    Rank = "silver"
	RankGold      Rank = "gold"
	RankPlatinum  Rank = "platinum"
	RankDiamond   Rank = "diamond"
	RankAscendant Rank = "ascendant"
	RankImmortal  Rank = "immortal"
	RankRadiant   Rank = "radiant"
)

type RankSource string

const (
	RankSourceTitle          RankSource = "title"
	RankSourceSearchMetadata RankSource = "search_metadata"
)

type Severity string

const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
)

type VOD struct {
	Line         int
	EnabledRaw   string
	Enabled      bool
	Rank         Rank
	Label        string
	VideoID      string
	URL          string
	DurationRaw  string
	Duration     time.Duration
	Title        string
	Channel      string
	RankSource   RankSource
	Notes        string
	ManifestPath string
}

type Issue struct {
	Line     int
	Severity Severity
	Field    string
	Message  string
}

func LoadManifest(path string) ([]VOD, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	vods, err := ParseManifest(file)
	if err != nil {
		return nil, err
	}

	for i := range vods {
		vods[i].ManifestPath = path
	}

	return vods, nil
}

func ParseManifest(r io.Reader) ([]VOD, error) {
	reader := csv.NewReader(r)
	reader.Comma = '\t'
	reader.Comment = '#'
	reader.FieldsPerRecord = -1
	reader.TrimLeadingSpace = true

	var vods []VOD
	for {
		row, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read manifest row: %w", err)
		}
		if len(row) == 1 && strings.TrimSpace(row[0]) == "" {
			continue
		}
		if len(row) != ManifestColumns {
			line, _ := reader.FieldPos(0)
			return nil, fmt.Errorf("line %d: expected %d columns, got %d", line, ManifestColumns, len(row))
		}

		line, _ := reader.FieldPos(0)
		enabledRaw := strings.TrimSpace(row[0])
		durationRaw := strings.TrimSpace(row[5])
		duration, _ := ParseClockDuration(durationRaw)

		vods = append(vods, VOD{
			Line:        line,
			EnabledRaw:  enabledRaw,
			Enabled:     enabledRaw == "1",
			Rank:        Rank(strings.TrimSpace(row[1])),
			Label:       strings.TrimSpace(row[2]),
			VideoID:     strings.TrimSpace(row[3]),
			URL:         strings.TrimSpace(row[4]),
			DurationRaw: durationRaw,
			Duration:    duration,
			Title:       strings.TrimSpace(row[6]),
			Channel:     strings.TrimSpace(row[7]),
			RankSource:  RankSource(strings.TrimSpace(row[8])),
			Notes:       strings.TrimSpace(row[9]),
		})
	}

	return vods, nil
}

func ParseClockDuration(value string) (time.Duration, error) {
	parts := strings.Split(value, ":")
	if len(parts) != 2 && len(parts) != 3 {
		return 0, fmt.Errorf("expected mm:ss or hh:mm:ss, got %q", value)
	}

	var nums []int
	for _, part := range parts {
		n, err := strconv.Atoi(part)
		if err != nil {
			return 0, fmt.Errorf("invalid duration component %q: %w", part, err)
		}
		nums = append(nums, n)
	}

	var hours, minutes, seconds int
	if len(nums) == 2 {
		minutes = nums[0]
		seconds = nums[1]
	} else {
		hours = nums[0]
		minutes = nums[1]
		seconds = nums[2]
	}

	if minutes < 0 || seconds < 0 || seconds > 59 {
		return 0, fmt.Errorf("invalid duration %q", value)
	}
	if len(nums) == 3 && minutes > 59 {
		return 0, fmt.Errorf("invalid duration %q", value)
	}

	return time.Duration(hours)*time.Hour + time.Duration(minutes)*time.Minute + time.Duration(seconds)*time.Second, nil
}

func Validate(vods []VOD) []Issue {
	var issues []Issue
	labels := make(map[string]int)
	videoIDs := make(map[string]int)

	for _, vod := range vods {
		if vod.EnabledRaw != "0" && vod.EnabledRaw != "1" {
			issues = append(issues, Issue{Line: vod.Line, Severity: SeverityError, Field: "enabled", Message: "must be 0 or 1"})
		}

		if !IsValidRank(vod.Rank) {
			issues = append(issues, Issue{Line: vod.Line, Severity: SeverityError, Field: "rank", Message: fmt.Sprintf("unknown rank %q", vod.Rank)})
		}

		if !labelPattern.MatchString(vod.Label) {
			issues = append(issues, Issue{Line: vod.Line, Severity: SeverityError, Field: "label", Message: "must match ^[a-z0-9_]+$"})
		}

		if vod.Label == "" {
			issues = append(issues, Issue{Line: vod.Line, Severity: SeverityError, Field: "label", Message: "is required"})
		} else if firstLine, ok := labels[vod.Label]; ok {
			issues = append(issues, Issue{Line: vod.Line, Severity: SeverityError, Field: "label", Message: fmt.Sprintf("duplicate label, first seen on line %d", firstLine)})
		} else {
			labels[vod.Label] = vod.Line
		}

		if vod.VideoID == "" {
			issues = append(issues, Issue{Line: vod.Line, Severity: SeverityError, Field: "video_id", Message: "is required"})
		} else if firstLine, ok := videoIDs[vod.VideoID]; ok {
			issues = append(issues, Issue{Line: vod.Line, Severity: SeverityError, Field: "video_id", Message: fmt.Sprintf("duplicate video ID, first seen on line %d", firstLine)})
		} else {
			videoIDs[vod.VideoID] = vod.Line
		}

		if err := validateYouTubeURL(vod.URL); err != nil {
			issues = append(issues, Issue{Line: vod.Line, Severity: SeverityError, Field: "url", Message: err.Error()})
		}

		if _, err := ParseClockDuration(vod.DurationRaw); err != nil {
			issues = append(issues, Issue{Line: vod.Line, Severity: SeverityError, Field: "duration", Message: err.Error()})
		}

		if vod.Title == "" {
			issues = append(issues, Issue{Line: vod.Line, Severity: SeverityWarning, Field: "title", Message: "is empty"})
		}

		if vod.Channel == "" {
			issues = append(issues, Issue{Line: vod.Line, Severity: SeverityWarning, Field: "channel", Message: "is empty"})
		}

		if !IsValidRankSource(vod.RankSource) {
			issues = append(issues, Issue{Line: vod.Line, Severity: SeverityError, Field: "rank_source", Message: fmt.Sprintf("unknown rank source %q", vod.RankSource)})
		}

		if vod.RankSource == RankSourceSearchMetadata {
			issues = append(issues, Issue{Line: vod.Line, Severity: SeverityWarning, Field: "rank_source", Message: "rank should be manually checked from HUD"})
		}
	}

	return issues
}

func IsValidRank(rank Rank) bool {
	switch rank {
	case RankIron, RankBronze, RankSilver, RankGold, RankPlatinum, RankDiamond, RankAscendant, RankImmortal, RankRadiant:
		return true
	default:
		return false
	}
}

func IsValidRankSource(source RankSource) bool {
	switch source {
	case RankSourceTitle, RankSourceSearchMetadata:
		return true
	default:
		return false
	}
}

func CountEnabled(vods []VOD) int {
	var count int
	for _, vod := range vods {
		if vod.Enabled {
			count++
		}
	}
	return count
}

func HasErrors(issues []Issue) bool {
	for _, issue := range issues {
		if issue.Severity == SeverityError {
			return true
		}
	}
	return false
}

func Filter(vods []VOD, rank Rank, enabledOnly bool) []VOD {
	var filtered []VOD
	for _, vod := range vods {
		if rank != "" && vod.Rank != rank {
			continue
		}
		if enabledOnly && !vod.Enabled {
			continue
		}
		filtered = append(filtered, vod)
	}
	return filtered
}

func FindByLabel(vods []VOD, label string) (VOD, bool) {
	for _, vod := range vods {
		if vod.Label == label {
			return vod, true
		}
	}
	return VOD{}, false
}

func VideoFilename(vod VOD, ext string) string {
	return fmt.Sprintf("%s__%s%s", vod.Label, vod.VideoID, ext)
}

func VideoPath(rawRoot string, vod VOD, ext string) string {
	return filepath.Join(rawRoot, string(vod.Rank), VideoFilename(vod, ext))
}

func validateYouTubeURL(value string) error {
	parsed, err := url.Parse(value)
	if err != nil {
		return err
	}
	if parsed.Scheme != "https" && parsed.Scheme != "http" {
		return fmt.Errorf("unsupported scheme %q", parsed.Scheme)
	}

	host := strings.TrimPrefix(strings.ToLower(parsed.Hostname()), "www.")
	if host != "youtube.com" && host != "youtu.be" {
		return fmt.Errorf("unsupported host %q", parsed.Hostname())
	}

	return nil
}
