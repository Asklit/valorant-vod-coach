package vision

import (
	"context"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"math"
	"os"

	"github.com/asklit/valorant-vod-coach/internal/domain"
)

type imageSignature []float64

type frameSignature struct {
	global   imageSignature
	killfeed imageSignature
	damage   imageSignature
}

type regionStats struct {
	brightness  float64
	contrast    float64
	edgeDensity float64
	whiteSignal float64
	darkSignal  float64
	redSignal   float64
	tealSignal  float64
}

func collectFrameObservations(ctx context.Context, frames []domain.Frame) ([]domain.FrameObservation, int) {
	observations := make([]domain.FrameObservation, 0, len(frames))
	skipped := 0
	var previous frameSignature

	for _, frame := range frames {
		if err := ctx.Err(); err != nil {
			break
		}

		observation, signature, err := analyzeFrame(frame, previous)
		if err != nil {
			skipped++
			continue
		}
		previous = signature
		observations = append(observations, observation)
	}

	return observations, skipped
}

func analyzeFrame(frame domain.Frame, previous frameSignature) (domain.FrameObservation, frameSignature, error) {
	file, err := os.Open(frame.Path)
	if err != nil {
		return domain.FrameObservation{}, frameSignature{}, err
	}
	defer file.Close()

	img, _, err := image.Decode(file)
	if err != nil {
		return domain.FrameObservation{}, frameSignature{}, err
	}

	bounds := img.Bounds()
	if bounds.Dx() <= 0 || bounds.Dy() <= 0 {
		return domain.FrameObservation{}, frameSignature{}, fmt.Errorf("empty image bounds")
	}

	global := measureRegion(img, bounds)
	center := measureRegion(img, relativeRect(bounds, 0.31, 0.24, 0.69, 0.77))
	minimap := measureRegion(img, relativeRect(bounds, 0.01, 0.015, 0.245, 0.36))
	timer := measureRegion(img, relativeRect(bounds, 0.43, 0.005, 0.57, 0.105))
	teamBar := measureRegion(img, relativeRect(bounds, 0.20, 0.0, 0.80, 0.115))
	healthHUD := measureRegion(img, relativeRect(bounds, 0.255, 0.88, 0.37, 0.995))
	abilityHUD := measureRegion(img, relativeRect(bounds, 0.365, 0.88, 0.635, 0.995))
	ammoHUD := measureRegion(img, relativeRect(bounds, 0.63, 0.88, 0.745, 0.995))
	damageRect := relativeRect(bounds, 0.34, 0.10, 0.66, 0.42)
	damageArea := measureRegion(img, damageRect)
	scoreboard := measureRegion(img, relativeRect(bounds, 0.22, 0.17, 0.78, 0.84))
	combatReport := measureRegion(img, relativeRect(bounds, 0.755, 0.23, 0.995, 0.84))
	buyBanner := measureRegion(img, relativeRect(bounds, 0.35, 0.11, 0.65, 0.27))

	signature := frameSignature{
		global:   makeRegionSignature(img, bounds, 48, 27),
		killfeed: makeRegionSignature(img, relativeRect(bounds, 0.72, 0.045, 0.995, 0.31), 18, 14),
		damage:   makeRedMaskSignature(img, damageRect, 24, 16),
	}
	motion := motionScore(previous.global, signature.global)
	killfeedChange := motionScore(previous.killfeed, signature.killfeed)
	damageChange := motionScore(previous.damage, signature.damage)

	centerActivity := clamp01(center.contrast*0.46 + center.edgeDensity*0.38 + center.redSignal*1.8)
	minimapSignal := clamp01((minimap.contrast*0.43 + minimap.edgeDensity*0.42 + scaledColor(minimap, 5)*0.15) * 1.14)

	timerGlyph := glyphSignal(timer)
	teamColor := clamp01(math.Min(1, teamBar.redSignal*11)*0.5 + math.Min(1, teamBar.tealSignal*11)*0.5)
	bottomGlyph := (glyphSignal(healthHUD) + glyphSignal(abilityHUD) + glyphSignal(ammoHUD)) / 3
	minimapLayout := clamp01(minimapSignal*0.78 + glyphSignal(minimap)*0.22)
	hudLayout := clamp01((timerGlyph*0.29 + teamColor*0.25 + bottomGlyph*0.28 + minimapLayout*0.18 - 0.08) * 1.22)
	hudSignal := clamp01(timerGlyph*0.26 + teamBar.edgeDensity*0.16 + bottomGlyph*0.36 + minimapLayout*0.22)

	killfeedStatic := killfeedRowsSignal(img, bounds)
	killfeedEvent := clamp01(killfeedStatic * clamp01(killfeedChange*1.55) * (0.30 + hudLayout*0.70) * 1.75)
	damageSignal := clamp01(sparseRedSignal(damageArea.redSignal) * (damageChange*0.67 + motion*0.13 + centerActivity*0.20))

	scoreboardSignal := scoreboardOverlaySignal(scoreboard)
	combatReportSignal := combatReportOverlaySignal(combatReport)
	buyPhaseSignal := buyPhaseOverlaySignal(buyBanner, hudLayout)
	overlaySuppression := math.Max(scoreboardSignal, math.Max(combatReportSignal*0.72, buyPhaseSignal))
	corroboration := math.Max(killfeedEvent, damageSignal)
	combatSignal := clamp01((motion*0.16 + centerActivity*0.19 + killfeedEvent*0.36 + damageSignal*0.29) * (0.24 + hudLayout*0.76))
	combatSignal *= 1 - clamp01(overlaySuppression)*0.88
	if corroboration < 0.18 {
		combatSignal *= 0.35
	}

	return domain.FrameObservation{
		Index:               frame.Index,
		TimestampSeconds:    round3(frame.TimestampSeconds),
		Path:                frame.Path,
		Brightness:          round4(global.brightness),
		Contrast:            round4(global.contrast),
		MotionScore:         round4(motion),
		CenterActivity:      round4(centerActivity),
		MinimapSignal:       round4(minimapSignal),
		HUDSignal:           round4(hudSignal),
		HUDLayoutConfidence: round4(hudLayout),
		KillfeedSignal:      round4(killfeedStatic),
		KillfeedChange:      round4(killfeedChange),
		KillfeedEventSignal: round4(killfeedEvent),
		DamageSignal:        round4(damageSignal),
		ScoreboardSignal:    round4(scoreboardSignal),
		CombatReportSignal:  round4(combatReportSignal),
		BuyPhaseSignal:      round4(buyPhaseSignal),
		CombatSignal:        round4(combatSignal),
		Phase:               "unknown",
	}, signature, nil
}

func glyphSignal(stats regionStats) float64 {
	return clamp01(stats.edgeDensity*0.56 + math.Min(1, stats.whiteSignal*9)*0.44)
}

func scaledColor(stats regionStats, scale float64) float64 {
	return clamp01((stats.redSignal + stats.tealSignal) * scale)
}

func killfeedRowsSignal(img image.Image, bounds image.Rectangle) float64 {
	best := 0.0
	for row := 0; row < 5; row++ {
		top := 0.052 + float64(row)*0.047
		stats := measureRegion(img, relativeRect(bounds, 0.72, top, 0.995, top+0.052))
		whiteText := textWhiteSignal(stats.whiteSignal)
		textGlyph := clamp01(stats.edgeDensity*0.64 + whiteText*0.36)
		color := scaledColor(stats, 8)
		raw := stats.edgeDensity*0.30 + textGlyph*0.43 + color*0.27
		gate := clamp01((textGlyph - 0.075) * 3.2)
		rowShape := clamp01((stats.edgeDensity - 0.048) * 12)
		signal := clamp01(raw * gate * rowShape * clamp01(0.35+color*0.65) * 1.75)
		if signal > best {
			best = signal
		}
	}
	return best
}

func textWhiteSignal(ratio float64) float64 {
	switch {
	case ratio <= 0.002:
		return 0
	case ratio <= 0.08:
		return clamp01(ratio / 0.035)
	case ratio <= 0.22:
		return clamp01(1 - (ratio-0.08)/0.14*0.55)
	case ratio <= 0.38:
		return clamp01(0.45 - (ratio-0.22)/0.16*0.45)
	default:
		return 0
	}
}

func sparseRedSignal(ratio float64) float64 {
	switch {
	case ratio <= 0.001:
		return 0
	case ratio <= 0.045:
		return clamp01(ratio / 0.025)
	case ratio <= 0.14:
		return clamp01(1 - (ratio-0.045)/0.095*0.45)
	case ratio <= 0.34:
		return clamp01(0.55 - (ratio-0.14)/0.20*0.50)
	default:
		return 0.03
	}
}

func scoreboardOverlaySignal(stats regionStats) float64 {
	redRows := clamp01(stats.redSignal * 18)
	tealRows := clamp01(stats.tealSignal * 18)
	rowColors := math.Min(redRows, tealRows)
	raw := rowColors*0.43 + glyphSignal(stats)*0.24 + stats.darkSignal*0.14 + stats.edgeDensity*0.19
	return clamp01((raw - 0.22) * 1.42)
}

func combatReportOverlaySignal(stats regionStats) float64 {
	panelColor := scaledColor(stats, 13)
	raw := panelColor*0.28 + glyphSignal(stats)*0.31 + stats.darkSignal*0.22 + stats.edgeDensity*0.19
	return clamp01((raw - 0.30) * 1.52)
}

func buyPhaseOverlaySignal(stats regionStats, hudLayout float64) float64 {
	raw := glyphSignal(stats)*0.55 + stats.darkSignal*0.18 + stats.edgeDensity*0.27
	return clamp01((raw-0.40)*1.8) * clamp01(0.3+hudLayout*0.7)
}

func measureRegion(img image.Image, rect image.Rectangle) regionStats {
	rect = rect.Intersect(img.Bounds())
	if rect.Empty() {
		return regionStats{}
	}

	step := max(1, min(rect.Dx(), rect.Dy())/72)
	var count, whiteCount, darkCount, redCount, tealCount int
	var sum, sumSq, edgeSum float64
	var edgePairs int

	for y := rect.Min.Y; y < rect.Max.Y; y += step {
		for x := rect.Min.X; x < rect.Max.X; x += step {
			r, g, b, _ := img.At(x, y).RGBA()
			r8 := float64(r >> 8)
			g8 := float64(g >> 8)
			b8 := float64(b >> 8)
			luma := 0.2126*r8 + 0.7152*g8 + 0.0722*b8
			sum += luma
			sumSq += luma * luma
			count++

			channelSpread := math.Max(r8, math.Max(g8, b8)) - math.Min(r8, math.Min(g8, b8))
			if luma >= 178 && channelSpread <= 58 {
				whiteCount++
			}
			if luma <= 58 {
				darkCount++
			}
			if r8 > 105 && r8 > g8*1.18 && r8 > b8*1.12 {
				redCount++
			}
			if g8 > 82 && b8 > 72 && g8 > r8*1.13 && b8 > r8*1.04 {
				tealCount++
			}
			if x+step < rect.Max.X {
				edgeSum += math.Abs(luma-lumaAt(img, x+step, y)) / 255
				edgePairs++
			}
			if y+step < rect.Max.Y {
				edgeSum += math.Abs(luma-lumaAt(img, x, y+step)) / 255
				edgePairs++
			}
		}
	}

	if count == 0 {
		return regionStats{}
	}

	mean := sum / float64(count)
	variance := sumSq/float64(count) - mean*mean
	if variance < 0 {
		variance = 0
	}

	edgeDensity := 0.0
	if edgePairs > 0 {
		edgeDensity = clamp01((edgeSum / float64(edgePairs)) * 3.2)
	}

	return regionStats{
		brightness:  clamp01(mean / 255),
		contrast:    clamp01(math.Sqrt(variance) / 92),
		edgeDensity: edgeDensity,
		whiteSignal: float64(whiteCount) / float64(count),
		darkSignal:  float64(darkCount) / float64(count),
		redSignal:   float64(redCount) / float64(count),
		tealSignal:  float64(tealCount) / float64(count),
	}
}

func makeRegionSignature(img image.Image, bounds image.Rectangle, cols, rows int) imageSignature {
	bounds = bounds.Intersect(img.Bounds())
	if bounds.Empty() {
		return nil
	}

	signature := make(imageSignature, 0, cols*rows)
	for row := 0; row < rows; row++ {
		y := bounds.Min.Y + int((float64(row)+0.5)*float64(bounds.Dy())/float64(rows))
		if y >= bounds.Max.Y {
			y = bounds.Max.Y - 1
		}
		for col := 0; col < cols; col++ {
			x := bounds.Min.X + int((float64(col)+0.5)*float64(bounds.Dx())/float64(cols))
			if x >= bounds.Max.X {
				x = bounds.Max.X - 1
			}
			signature = append(signature, lumaAt(img, x, y))
		}
	}
	return signature
}

func makeRedMaskSignature(img image.Image, bounds image.Rectangle, cols, rows int) imageSignature {
	bounds = bounds.Intersect(img.Bounds())
	if bounds.Empty() {
		return nil
	}

	signature := make(imageSignature, 0, cols*rows)
	for row := 0; row < rows; row++ {
		y := bounds.Min.Y + int((float64(row)+0.5)*float64(bounds.Dy())/float64(rows))
		if y >= bounds.Max.Y {
			y = bounds.Max.Y - 1
		}
		for col := 0; col < cols; col++ {
			x := bounds.Min.X + int((float64(col)+0.5)*float64(bounds.Dx())/float64(cols))
			if x >= bounds.Max.X {
				x = bounds.Max.X - 1
			}
			r, g, b, _ := img.At(x, y).RGBA()
			r8, g8, b8 := float64(r>>8), float64(g>>8), float64(b>>8)
			value := 0.0
			if r8 > 105 && r8 > g8*1.18 && r8 > b8*1.12 {
				value = 255
			}
			signature = append(signature, value)
		}
	}
	return signature
}

func motionScore(previous, current imageSignature) float64 {
	if len(previous) == 0 || len(previous) != len(current) {
		return 0
	}

	var diff float64
	for index := range current {
		diff += math.Abs(current[index] - previous[index])
	}
	return clamp01((diff / float64(len(current)) / 255) * 3.6)
}

func relativeRect(bounds image.Rectangle, left, top, right, bottom float64) image.Rectangle {
	width := bounds.Dx()
	height := bounds.Dy()
	minX := bounds.Min.X + int(left*float64(width))
	minY := bounds.Min.Y + int(top*float64(height))
	maxX := bounds.Min.X + int(right*float64(width))
	maxY := bounds.Min.Y + int(bottom*float64(height))
	if maxX <= minX {
		maxX = minX + 1
	}
	if maxY <= minY {
		maxY = minY + 1
	}
	return image.Rect(minX, minY, maxX, maxY).Intersect(bounds)
}

func lumaAt(img image.Image, x, y int) float64 {
	r, g, b, _ := img.At(x, y).RGBA()
	r8 := float64(r >> 8)
	g8 := float64(g >> 8)
	b8 := float64(b >> 8)
	return 0.2126*r8 + 0.7152*g8 + 0.0722*b8
}
