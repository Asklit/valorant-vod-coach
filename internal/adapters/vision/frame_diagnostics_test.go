package vision

import (
	"context"
	"image"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/asklit/valorant-vod-coach/internal/domain"
)

func TestFrameOCRDiagnostics(t *testing.T) {
	specs := strings.Split(strings.TrimSpace(os.Getenv("VALORANT_OCR_DIAGNOSTIC_FRAMES")), ",")
	if len(specs) == 0 || specs[0] == "" {
		t.Skip("set VALORANT_OCR_DIAGNOSTIC_FRAMES to expected-signal=path entries")
	}
	tesseractPath, err := exec.LookPath("tesseract")
	if err != nil {
		t.Skip("tesseract is unavailable")
	}
	for _, spec := range specs {
		expected, path, found := strings.Cut(strings.TrimSpace(spec), "=")
		if !found || expected == "" || path == "" {
			t.Fatalf("invalid diagnostic spec %q", spec)
		}
		signals, err := inspectFrameText(context.Background(), tesseractPath, ocrTask{
			frame: domain.FrameObservation{Path: path}, buy: true, scoreboard: true, report: true,
		})
		if err != nil {
			t.Fatalf("inspect %s: %v", path, err)
		}
		if !containsString(signals, expected) {
			t.Fatalf("%s signals = %v, expected %q", path, signals, expected)
		}
		if expected == ocrSignalCombatReport && containsString(signals, ocrSignalDeathReport) {
			t.Fatalf("%s generic report was incorrectly classified as a POV death: %v", path, signals)
		}
		t.Logf("%s signals = %v", path, signals)
	}
}

func TestFrameRegionDiagnostics(t *testing.T) {
	paths := strings.Split(strings.TrimSpace(os.Getenv("VALORANT_DIAGNOSTIC_FRAMES")), ",")
	if len(paths) == 0 || paths[0] == "" {
		t.Skip("set VALORANT_DIAGNOSTIC_FRAMES to inspect comma-separated frame paths")
	}

	for _, path := range paths {
		file, err := os.Open(strings.TrimSpace(path))
		if err != nil {
			t.Fatalf("open %s: %v", path, err)
		}
		img, _, err := image.Decode(file)
		file.Close()
		if err != nil {
			t.Fatalf("decode %s: %v", path, err)
		}

		bounds := img.Bounds()
		regions := map[string]regionStats{
			"killfeed":      measureRegion(img, relativeRect(bounds, 0.72, 0.045, 0.995, 0.31)),
			"damage":        measureRegion(img, relativeRect(bounds, 0.27, 0.11, 0.73, 0.58)),
			"scoreboard":    measureRegion(img, relativeRect(bounds, 0.22, 0.17, 0.78, 0.84)),
			"combat_report": measureRegion(img, relativeRect(bounds, 0.755, 0.23, 0.995, 0.84)),
			"buy_banner":    measureRegion(img, relativeRect(bounds, 0.35, 0.11, 0.65, 0.27)),
		}
		for name, stats := range regions {
			t.Logf("%s %s brightness=%.4f contrast=%.4f edge=%.4f white=%.4f dark=%.4f red=%.4f teal=%.4f glyph=%.4f", path, name, stats.brightness, stats.contrast, stats.edgeDensity, stats.whiteSignal, stats.darkSignal, stats.redSignal, stats.tealSignal, glyphSignal(stats))
		}
		for row := 0; row < 5; row++ {
			top := 0.052 + float64(row)*0.047
			stats := measureRegion(img, relativeRect(bounds, 0.72, top, 0.995, top+0.052))
			t.Logf("%s killfeed_row_%d edge=%.4f white=%.4f red=%.4f teal=%.4f text_white=%.4f", path, row, stats.edgeDensity, stats.whiteSignal, stats.redSignal, stats.tealSignal, textWhiteSignal(stats.whiteSignal))
		}
	}
}

func TestOCRSemanticPatterns(t *testing.T) {
	tests := []struct {
		name  string
		text  string
		match func(string) bool
	}{
		{name: "english buy", text: "BUY PHASE PRESS B TO BUY", match: isBuyPhaseText},
		{name: "turkish buy", text: "SATIN ALMA EVRESI", match: isBuyPhaseText},
		{name: "english report", text: "KILLED BY REYNA COMBAT REPORT", match: isCombatReportText},
		{name: "english death report", text: "KILLED BY REYNA COMBAT REPORT INCOMING KILLED YOU", match: isDeathReportText},
		{name: "turkish death report", text: "OLDUREN REYNA CATISMA RAPORU", match: isDeathReportText},
		{name: "turkish report OCR", text: "GATISMA RAPORU", match: isCombatReportText},
		{name: "english scoreboard", text: "NAME ULTIMATE KDA LOADOUT CREDS PING", match: isScoreboardText},
		{name: "english round victory", text: "VICTORY ENEMY TEAM ELIMINATED", match: isRoundEndText},
		{name: "turkish round victory", text: "KAZANDINIZ DUSMAN TAKIM YOK EDILDI", match: isRoundEndText},
		{name: "partial Turkish round subtitle", text: "ISMAN TAKIM YOK EDILDI", match: isRoundEndText},
		{name: "turkish flawless", text: "TERTEMIZ", match: isRoundEndText},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if !test.match(normalizeOCRText(test.text)) {
				t.Fatalf("expected semantic OCR match for %q", test.text)
			}
		})
	}
	if isCombatReportText(normalizeOCRText("red wall and weapon")) || isBuyPhaseText(normalizeOCRText("spike planted")) || isRoundEndText(normalizeOCRText("round in progress")) {
		t.Fatalf("ordinary gameplay text must not match HUD overlays")
	}
	if isDeathReportText(normalizeOCRText("OUTGOING COMBAT REPORT 135 KILLED")) {
		t.Fatal("outgoing combat report rows must not confirm a POV death")
	}
}

func TestPropagateRoundEndOCRState(t *testing.T) {
	observations := []domain.FrameObservation{
		{TimestampSeconds: 72},
		{TimestampSeconds: 75, OCRSignals: []string{ocrSignalRoundEnd}},
		{TimestampSeconds: 78},
		{TimestampSeconds: 81},
	}
	propagateConfirmedOCRStates(observations)

	if observations[0].RoundEndSignal < 0.90 || observations[2].RoundEndSignal < 0.90 {
		t.Fatalf("expected neighboring frames to inherit round-end state: %+v", observations)
	}
	if observations[3].RoundEndSignal != 0 {
		t.Fatalf("round-end state must remain locally bounded: %+v", observations)
	}
}
