package dataset

import (
	"strings"
	"testing"
	"time"
)

func TestParseManifest(t *testing.T) {
	input := strings.NewReader(`# enabled	rank	label	video_id	url	duration	title	channel	rank_source	notes
1	diamond	diamond_example	abc123	https://www.youtube.com/watch?v=abc123	37:04	Diamond VOD	Channel	title	game_vod_20_40
0	iron	iron_example	def456	https://youtu.be/def456	1:02:03	Iron VOD	Channel	search_metadata	check_rank
`)

	vods, err := ParseManifest(input)
	if err != nil {
		t.Fatalf("ParseManifest returned error: %v", err)
	}

	if len(vods) != 2 {
		t.Fatalf("expected 2 vods, got %d", len(vods))
	}

	if got := vods[0].Duration; got != 37*time.Minute+4*time.Second {
		t.Fatalf("unexpected first duration: %v", got)
	}

	if got := vods[1].Duration; got != time.Hour+2*time.Minute+3*time.Second {
		t.Fatalf("unexpected second duration: %v", got)
	}

	if vods[1].Enabled {
		t.Fatal("expected second VOD to be disabled")
	}
}

func TestValidateValidManifestWithWarning(t *testing.T) {
	input := strings.NewReader(`1	platinum	platinum_example	abc123	https://www.youtube.com/watch?v=abc123	24:24	Pearl ranked	Channel	search_metadata	manual check
`)

	vods, err := ParseManifest(input)
	if err != nil {
		t.Fatalf("ParseManifest returned error: %v", err)
	}

	issues := Validate(vods)
	if HasErrors(issues) {
		t.Fatalf("expected no errors, got %#v", issues)
	}

	if len(issues) != 1 || issues[0].Severity != SeverityWarning {
		t.Fatalf("expected one warning, got %#v", issues)
	}
}

func TestValidateDuplicateVideoID(t *testing.T) {
	input := strings.NewReader(`1	gold	gold_one	abc123	https://www.youtube.com/watch?v=abc123	20:00	One	Channel	title	ok
1	gold	gold_two	abc123	https://www.youtube.com/watch?v=abc123	21:00	Two	Channel	title	ok
`)

	vods, err := ParseManifest(input)
	if err != nil {
		t.Fatalf("ParseManifest returned error: %v", err)
	}

	issues := Validate(vods)
	if !HasErrors(issues) {
		t.Fatalf("expected duplicate video ID error, got %#v", issues)
	}
}

func TestFindByLabel(t *testing.T) {
	vods := []VOD{
		{Label: "first"},
		{Label: "second", VideoID: "abc123"},
	}

	vod, ok := FindByLabel(vods, "second")
	if !ok {
		t.Fatal("expected VOD to be found")
	}

	if vod.VideoID != "abc123" {
		t.Fatalf("unexpected VOD: %#v", vod)
	}
}

func TestParseClockDurationRejectsInvalidValue(t *testing.T) {
	if _, err := ParseClockDuration("10:99"); err == nil {
		t.Fatal("expected invalid seconds to fail")
	}
}
