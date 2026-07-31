package domain

const CoachReviewSchemaVersion = 1

type CoachReview struct {
	SchemaVersion   int                  `json:"schema_version"`
	Engine          string               `json:"engine"`
	Method          string               `json:"method"`
	Status          string               `json:"status"`
	Summary         string               `json:"summary"`
	EvidenceQuality CoachEvidenceQuality `json:"evidence_quality"`
	Decisions       []CoachDecision      `json:"decisions"`
	Limitations     []string             `json:"limitations,omitempty"`
}

type CoachEvidenceQuality struct {
	Score            float64 `json:"score"`
	Level            string  `json:"level"`
	FrameCoverage    float64 `json:"frame_coverage"`
	HUDSignal        float64 `json:"hud_signal,omitempty"`
	MinimapSignal    float64 `json:"minimap_signal,omitempty"`
	HasAudio         bool    `json:"has_audio"`
	MacroReviewReady bool    `json:"macro_review_ready"`
	MicroReviewReady bool    `json:"micro_review_ready"`
}

type CoachDecision struct {
	ID                  string                `json:"id"`
	RuleID              string                `json:"rule_id"`
	WindowID            string                `json:"window_id"`
	Kind                string                `json:"kind"`
	Assessment          string                `json:"assessment"`
	Severity            FindingSeverity       `json:"severity"`
	Title               string                `json:"title"`
	Observation         string                `json:"observation"`
	WhyReview           string                `json:"why_review"`
	Confidence          float64               `json:"confidence"`
	TimestampSeconds    float64               `json:"timestamp_seconds"`
	StartSeconds        float64               `json:"start_seconds"`
	EndSeconds          float64               `json:"end_seconds"`
	RoundNumber         int                   `json:"round_number,omitempty"`
	ClipPath            string                `json:"clip_path,omitempty"`
	ClipDurationSeconds float64               `json:"clip_duration_seconds,omitempty"`
	Evidence            []EvidenceRef         `json:"evidence,omitempty"`
	Requirements        []EvidenceRequirement `json:"requirements,omitempty"`
	Questions           []CoachQuestion       `json:"questions,omitempty"`
	Recommendation      *CoachRecommendation  `json:"recommendation,omitempty"`
	Tags                []string              `json:"tags,omitempty"`
}

type EvidenceRequirement struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
}

type CoachQuestion struct {
	ID       string                `json:"id"`
	Prompt   string                `json:"prompt"`
	Required bool                  `json:"required"`
	Options  []CoachQuestionOption `json:"options"`
}

type CoachQuestionOption struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

type CoachRecommendation struct {
	Summary      string `json:"summary"`
	WhyItMatters string `json:"why_it_matters"`
	BetterAction string `json:"better_action"`
	Drill        string `json:"drill"`
	Checkpoint   string `json:"checkpoint"`
}
