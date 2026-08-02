package vision

import (
	"bytes"
	"context"
	"image"
	"image/draw"
	"image/png"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/asklit/valorant-vod-coach/internal/domain"
)

const (
	ocrSignalBuyPhase     = "buy_phase"
	ocrSignalScoreboard   = "scoreboard"
	ocrSignalCombatReport = "combat_report"
	ocrSignalDeathReport  = "death_report"
	ocrSignalRoundEnd     = "round_end"
)

type ocrTask struct {
	index      int
	frame      domain.FrameObservation
	buy        bool
	scoreboard bool
	report     bool
}

type ocrResult struct {
	index   int
	signals []string
	err     error
}

func enrichFrameObservationsWithOCR(ctx context.Context, observations []domain.FrameObservation, tesseractPath string) (string, int) {
	if strings.TrimSpace(tesseractPath) == "" {
		return "disabled", 0
	}
	path, err := exec.LookPath(tesseractPath)
	if err != nil {
		return "unavailable", 0
	}

	tasks := selectOCRTasks(observations)
	if len(tasks) == 0 {
		return "completed", 0
	}

	workerCount := min(2, len(tasks))
	taskChannel := make(chan ocrTask)
	resultChannel := make(chan ocrResult, len(tasks))
	var workers sync.WaitGroup
	workers.Add(workerCount)
	for worker := 0; worker < workerCount; worker++ {
		go func() {
			defer workers.Done()
			for task := range taskChannel {
				signals, taskErr := inspectFrameText(ctx, path, task)
				resultChannel <- ocrResult{index: task.index, signals: signals, err: taskErr}
			}
		}()
	}

	go func() {
		defer close(taskChannel)
		for _, task := range tasks {
			select {
			case taskChannel <- task:
			case <-ctx.Done():
				return
			}
		}
	}()
	go func() {
		workers.Wait()
		close(resultChannel)
	}()

	analyzed := 0
	hadErrors := false
	for result := range resultChannel {
		if result.err != nil {
			hadErrors = true
			continue
		}
		analyzed++
		observations[result.index].OCRSignals = compactStrings(result.signals...)
		for _, signal := range result.signals {
			switch signal {
			case ocrSignalBuyPhase:
				observations[result.index].BuyPhaseSignal = mathMax(observations[result.index].BuyPhaseSignal, 0.96)
			case ocrSignalScoreboard:
				observations[result.index].ScoreboardSignal = mathMax(observations[result.index].ScoreboardSignal, 0.94)
			case ocrSignalCombatReport:
				observations[result.index].CombatReportSignal = mathMax(observations[result.index].CombatReportSignal, 0.98)
			case ocrSignalDeathReport:
				observations[result.index].CombatReportSignal = mathMax(observations[result.index].CombatReportSignal, 0.99)
			case ocrSignalRoundEnd:
				observations[result.index].RoundEndSignal = mathMax(observations[result.index].RoundEndSignal, 0.98)
			}
		}
	}
	propagateConfirmedOCRStates(observations)
	if err := ctx.Err(); err != nil {
		return "cancelled", analyzed
	}
	if hadErrors {
		return "completed_with_errors", analyzed
	}
	return "completed", analyzed
}

func selectOCRTasks(observations []domain.FrameObservation) []ocrTask {
	tasks := make([]ocrTask, 0, len(observations)/4)
	lastSparseBucket := -1
	for index, observation := range observations {
		bucket := int(observation.TimestampSeconds / 3)
		sparse := bucket != lastSparseBucket
		if sparse {
			lastSparseBucket = bucket
		}
		previousScoreboard := 0.0
		previousReport := 0.0
		if index > 0 {
			previousScoreboard = observations[index-1].ScoreboardSignal
			previousReport = observations[index-1].CombatReportSignal
		}

		buyCandidate := sparse || observation.BuyPhaseSignal >= 0.08
		scoreboardCandidate := observation.ScoreboardSignal >= 0.24 && (previousScoreboard < 0.24 || sparse)
		reportCandidate := observation.CombatReportSignal >= 0.12 && (previousReport < 0.12 || sparse)
		if !buyCandidate && !scoreboardCandidate && !reportCandidate {
			continue
		}
		tasks = append(tasks, ocrTask{
			index:      index,
			frame:      observation,
			buy:        buyCandidate,
			scoreboard: scoreboardCandidate,
			report:     reportCandidate,
		})
	}
	return tasks
}

func inspectFrameText(ctx context.Context, tesseractPath string, task ocrTask) ([]string, error) {
	file, err := os.Open(task.frame.Path)
	if err != nil {
		return nil, err
	}
	img, _, err := image.Decode(file)
	file.Close()
	if err != nil {
		return nil, err
	}

	bounds := img.Bounds()
	signals := make([]string, 0, 3)
	if task.buy {
		text, runErr := recognizeRegion(ctx, tesseractPath, img, relativeRect(bounds, 0.35, 0.09, 0.65, 0.29), "12")
		if runErr != nil {
			return nil, runErr
		}
		if isBuyPhaseText(text) {
			signals = append(signals, ocrSignalBuyPhase)
		}
		if isRoundEndText(text) {
			signals = append(signals, ocrSignalRoundEnd)
		}
	}
	if task.scoreboard {
		text, runErr := recognizeRegion(ctx, tesseractPath, img, relativeRect(bounds, 0.22, 0.16, 0.78, 0.85), "11")
		if runErr != nil {
			return nil, runErr
		}
		if isScoreboardText(text) {
			signals = append(signals, ocrSignalScoreboard)
		}
	}
	if task.report {
		text, runErr := recognizeRegion(ctx, tesseractPath, img, relativeRect(bounds, 0.75, 0.20, 0.995, 0.86), "11")
		if runErr != nil {
			return nil, runErr
		}
		if isCombatReportText(text) {
			signals = append(signals, ocrSignalCombatReport)
		}
		if isDeathReportText(text) {
			signals = append(signals, ocrSignalDeathReport)
		}
	}
	return signals, nil
}

func recognizeRegion(ctx context.Context, tesseractPath string, img image.Image, rect image.Rectangle, pageSegmentationMode string) (string, error) {
	region := image.NewRGBA(image.Rect(0, 0, rect.Dx(), rect.Dy()))
	draw.Draw(region, region.Bounds(), img, rect.Min, draw.Src)
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, region); err != nil {
		return "", err
	}

	commandContext, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	command := exec.CommandContext(commandContext, tesseractPath, "stdin", "stdout", "-l", "eng", "--psm", pageSegmentationMode)
	command.Stdin = &encoded
	command.Stderr = nil
	command.Env = append(os.Environ(), "OMP_THREAD_LIMIT=1")
	output, err := command.Output()
	if err != nil {
		return "", err
	}
	return normalizeOCRText(string(output)), nil
}

func normalizeOCRText(text string) string {
	text = strings.ToUpper(text)
	replacer := strings.NewReplacer("|", "I", "0", "O", "Ç", "C", "Ş", "S", "Ğ", "G", "Ü", "U", "Ö", "O", "İ", "I")
	return strings.Join(strings.Fields(replacer.Replace(text)), " ")
}

func isBuyPhaseText(text string) bool {
	if containsAnyText(text,
		"BUY PHASE",
		"SATIN ALMA EVRES",
		"FASE DE COMPRA",
		"PHASE D ACHAT",
		"KAUFPHASE",
		"FASE DI ACQUISTO",
		"FAZA ZAKUPOW",
	) {
		return true
	}
	for _, token := range strings.Fields(text) {
		if editDistance(token, "SATIN") <= 1 {
			return true
		}
	}
	return false
}

func isCombatReportText(text string) bool {
	return containsAnyText(text,
		"COMBAT REPORT",
		"KILLED BY",
		"CATISMA RAPORU",
		"GATISMA RAPORU",
		"INFORME DE COMBATE",
		"RELATORIO DE COMBATE",
		"RAPPORT DE COMBAT",
		"KAMPFBERICHT",
	)
}

func isDeathReportText(text string) bool {
	return containsAnyText(text,
		"KILLED BY",
		"KILLED YOU",
		"OLDUREN",
		"SENI OLDURDU",
		"ASESINADO POR",
		"ABATIDO POR",
		"TUE PAR",
		"GETOTET VON",
	)
}

func isRoundEndText(text string) bool {
	return containsAnyText(text,
		"VICTORY",
		"DEFEAT",
		"FLAWLESS",
		"CLUTCH",
		"TEAM ELIMINATED",
		"SPIKE DETONATED",
		"SPIKE DEFUSED",
		"WON",
		"LOST",
		"KAZANDINIZ",
		"KAYBETTINIZ",
		"TERTEMIZ",
		"TAKIM YOK EDILDI",
		"SPIKE IMHA EDILDI",
		"SPIKE ETKISIZ",
		"VICTORIA",
		"DERROTA",
		"VICTOIRE",
		"DEFAITE",
		"SIEG",
		"NIEDERLAGE",
	)
}

func isScoreboardText(text string) bool {
	headers := []string{"NAME", "ULTIMATE", "KDA", "LOADOUT", "CREDS", "PING", "ULTI", "MUHIMMAT", "KREDI"}
	matches := 0
	for _, header := range headers {
		if strings.Contains(text, header) {
			matches++
		}
	}
	return matches >= 3
}

func containsAnyText(text string, candidates ...string) bool {
	for _, candidate := range candidates {
		if strings.Contains(text, candidate) {
			return true
		}
	}
	return false
}

func propagateConfirmedOCRStates(observations []domain.FrameObservation) {
	buyConfirmations := make([]int, 0)
	for index, observation := range observations {
		if containsString(observation.OCRSignals, ocrSignalBuyPhase) {
			buyConfirmations = append(buyConfirmations, index)
			for candidate := index; candidate >= 0 && observation.TimestampSeconds-observations[candidate].TimestampSeconds <= 5; candidate-- {
				observations[candidate].BuyPhaseSignal = mathMax(observations[candidate].BuyPhaseSignal, trustedBuyPhaseSignal)
			}
			for candidate := index + 1; candidate < len(observations) && observations[candidate].TimestampSeconds-observation.TimestampSeconds <= 5; candidate++ {
				observations[candidate].BuyPhaseSignal = mathMax(observations[candidate].BuyPhaseSignal, trustedBuyPhaseSignal)
			}
		}
		if containsString(observation.OCRSignals, ocrSignalScoreboard) {
			for candidate := index; candidate >= 0 && observation.TimestampSeconds-observations[candidate].TimestampSeconds <= 2; candidate-- {
				observations[candidate].ScoreboardSignal = mathMax(observations[candidate].ScoreboardSignal, trustedScoreboardSignal)
			}
			for candidate := index + 1; candidate < len(observations) && observations[candidate].TimestampSeconds-observation.TimestampSeconds <= 2; candidate++ {
				observations[candidate].ScoreboardSignal = mathMax(observations[candidate].ScoreboardSignal, trustedScoreboardSignal)
			}
		}
		if containsString(observation.OCRSignals, ocrSignalRoundEnd) {
			for candidate := index; candidate >= 0 && observation.TimestampSeconds-observations[candidate].TimestampSeconds <= 4; candidate-- {
				observations[candidate].RoundEndSignal = mathMax(observations[candidate].RoundEndSignal, 0.90)
			}
			for candidate := index + 1; candidate < len(observations) && observations[candidate].TimestampSeconds-observation.TimestampSeconds <= 4; candidate++ {
				observations[candidate].RoundEndSignal = mathMax(observations[candidate].RoundEndSignal, 0.90)
			}
		}
	}
	for confirmation := 1; confirmation < len(buyConfirmations); confirmation++ {
		previous := buyConfirmations[confirmation-1]
		current := buyConfirmations[confirmation]
		if observations[current].TimestampSeconds-observations[previous].TimestampSeconds > 32 {
			continue
		}
		for candidate := previous; candidate <= current; candidate++ {
			observations[candidate].BuyPhaseSignal = mathMax(observations[candidate].BuyPhaseSignal, trustedBuyPhaseSignal)
		}
	}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func editDistance(left, right string) int {
	leftRunes := []rune(left)
	rightRunes := []rune(right)
	previous := make([]int, len(rightRunes)+1)
	for index := range previous {
		previous[index] = index
	}
	for leftIndex, leftRune := range leftRunes {
		current := make([]int, len(rightRunes)+1)
		current[0] = leftIndex + 1
		for rightIndex, rightRune := range rightRunes {
			cost := 0
			if leftRune != rightRune {
				cost = 1
			}
			current[rightIndex+1] = min(current[rightIndex]+1, min(previous[rightIndex+1]+1, previous[rightIndex]+cost))
		}
		previous = current
	}
	return previous[len(rightRunes)]
}

func mathMax(left, right float64) float64 {
	if left > right {
		return left
	}
	return right
}
