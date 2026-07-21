package domain

import "time"

const AnalysisReportSchemaVersion = 3

type Rank string

type VOD struct {
	Label                   string  `json:"label"`
	VideoID                 string  `json:"video_id"`
	Rank                    Rank    `json:"rank"`
	SourceURL               string  `json:"source_url"`
	Title                   string  `json:"title"`
	Channel                 string  `json:"channel"`
	ManifestDurationSeconds float64 `json:"manifest_duration_seconds,omitempty"`
}

type MediaSummary struct {
	DurationSeconds float64 `json:"duration_seconds,omitempty"`
	HasDuration     bool    `json:"has_duration"`
	SizeBytes       int64   `json:"size_bytes,omitempty"`
	HasSize         bool    `json:"has_size"`
	VideoCodec      string  `json:"video_codec,omitempty"`
	Width           int     `json:"width,omitempty"`
	Height          int     `json:"height,omitempty"`
	FrameRate       string  `json:"frame_rate,omitempty"`
	AudioCodec      string  `json:"audio_codec,omitempty"`
	HasAudio        bool    `json:"has_audio"`
}

type FrameSampleSummary struct {
	Name             string  `json:"name"`
	OutputDir        string  `json:"output_dir"`
	ManifestPath     string  `json:"manifest_path"`
	FPS              string  `json:"fps"`
	FPSValue         float64 `json:"fps_value"`
	StartSeconds     float64 `json:"start_seconds"`
	DurationSeconds  float64 `json:"duration_seconds,omitempty"`
	FrameCount       int     `json:"frame_count"`
	Frames           []Frame `json:"frames,omitempty"`
	ContactSheetPath string  `json:"contact_sheet_path,omitempty"`
}

type Frame struct {
	Index            int     `json:"index"`
	TimestampSeconds float64 `json:"timestamp_seconds"`
	Path             string  `json:"path"`
}

type Artifact struct {
	Type   string `json:"type"`
	Format string `json:"format"`
	Path   string `json:"path"`
}

type FindingSeverity string

const (
	FindingSeverityInfo     FindingSeverity = "info"
	FindingSeverityLow      FindingSeverity = "low"
	FindingSeverityMedium   FindingSeverity = "medium"
	FindingSeverityHigh     FindingSeverity = "high"
	FindingSeverityCritical FindingSeverity = "critical"
)

type Finding struct {
	ID             string          `json:"id"`
	Severity       FindingSeverity `json:"severity"`
	Category       string          `json:"category"`
	Title          string          `json:"title"`
	Detail         string          `json:"detail"`
	Recommendation string          `json:"recommendation,omitempty"`
	Confidence     float64         `json:"confidence,omitempty"`
	Evidence       []EvidenceRef   `json:"evidence,omitempty"`
	Tags           []string        `json:"tags,omitempty"`
}

type EvidenceRef struct {
	ArtifactType     string  `json:"artifact_type"`
	Path             string  `json:"path"`
	TimestampSeconds float64 `json:"timestamp_seconds,omitempty"`
	FrameIndex       int     `json:"frame_index,omitempty"`
}

type TimelineEvent struct {
	TimestampSeconds float64 `json:"timestamp_seconds"`
	Type             string  `json:"type"`
	Title            string  `json:"title"`
	Detail           string  `json:"detail,omitempty"`
}

type GameplaySummary struct {
	Analyzer             string             `json:"analyzer,omitempty"`
	SampledFrames        int                `json:"sampled_frames"`
	AnalyzedFrames       int                `json:"analyzed_frames"`
	SkippedFrames        int                `json:"skipped_frames,omitempty"`
	ReviewWindowCount    int                `json:"review_window_count"`
	AverageMotionScore   float64            `json:"average_motion_score,omitempty"`
	AverageMinimapSignal float64            `json:"average_minimap_signal,omitempty"`
	AverageHUDSignal     float64            `json:"average_hud_signal,omitempty"`
	PeakCombatScore      float64            `json:"peak_combat_score,omitempty"`
	Coach                *CoachSummary      `json:"coach,omitempty"`
	PhaseProfile         []PhaseStat        `json:"phase_profile,omitempty"`
	FrameObservations    []FrameObservation `json:"frame_observations,omitempty"`
	ReviewWindows        []ReviewWindow     `json:"review_windows,omitempty"`
	Notes                []string           `json:"notes,omitempty"`
}

type CoachSummary struct {
	Verdict         string           `json:"verdict"`
	Confidence      float64          `json:"confidence"`
	CoverageSeconds float64          `json:"coverage_seconds,omitempty"`
	FocusAreas      []CoachFocusArea `json:"focus_areas,omitempty"`
	PracticePlan    []PracticeTask   `json:"practice_plan,omitempty"`
}

type CoachFocusArea struct {
	ID        string   `json:"id"`
	Priority  string   `json:"priority"`
	Category  string   `json:"category"`
	Title     string   `json:"title"`
	Detail    string   `json:"detail"`
	Score     float64  `json:"score"`
	WindowIDs []string `json:"window_ids,omitempty"`
}

type PracticeTask struct {
	ID      string   `json:"id"`
	Title   string   `json:"title"`
	Detail  string   `json:"detail"`
	Cadence string   `json:"cadence"`
	Tags    []string `json:"tags,omitempty"`
}

type PhaseStat struct {
	Phase string  `json:"phase"`
	Count int     `json:"count"`
	Ratio float64 `json:"ratio"`
}

type FrameObservation struct {
	Index            int     `json:"index"`
	TimestampSeconds float64 `json:"timestamp_seconds"`
	Path             string  `json:"path"`
	Brightness       float64 `json:"brightness"`
	Contrast         float64 `json:"contrast"`
	MotionScore      float64 `json:"motion_score"`
	CenterActivity   float64 `json:"center_activity"`
	MinimapSignal    float64 `json:"minimap_signal"`
	HUDSignal        float64 `json:"hud_signal"`
	CombatSignal     float64 `json:"combat_signal"`
	Phase            string  `json:"phase"`
}

type ReviewWindow struct {
	ID             string          `json:"id"`
	Kind           string          `json:"kind"`
	Severity       FindingSeverity `json:"severity"`
	Title          string          `json:"title"`
	Summary        string          `json:"summary"`
	Recommendation string          `json:"recommendation"`
	StartSeconds   float64         `json:"start_seconds"`
	EndSeconds     float64         `json:"end_seconds"`
	PeakSeconds    float64         `json:"peak_seconds"`
	Score          float64         `json:"score"`
	Evidence       []EvidenceRef   `json:"evidence,omitempty"`
	Tags           []string        `json:"tags,omitempty"`
}

type AnalysisReport struct {
	SchemaVersion int                 `json:"schema_version"`
	RunID         string              `json:"run_id"`
	Status        string              `json:"status"`
	GeneratedAt   time.Time           `json:"generated_at"`
	VOD           VOD                 `json:"vod"`
	Media         MediaSummary        `json:"media"`
	Sample        FrameSampleSummary  `json:"sample"`
	Gameplay      *GameplaySummary    `json:"gameplay,omitempty"`
	Findings      []Finding           `json:"findings"`
	Timeline      []TimelineEvent     `json:"timeline"`
	Artifacts     []Artifact          `json:"artifacts"`
	Metadata      AnalysisRunMetadata `json:"metadata"`
}

type AnalysisRunMetadata struct {
	Analyzer string `json:"analyzer"`
	Mode     string `json:"mode"`
}
