package dataset

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFindLocalVideo(t *testing.T) {
	root := t.TempDir()
	vod := VOD{
		Rank:    RankDiamond,
		Label:   "diamond_example",
		VideoID: "abc123",
	}

	rankDir := filepath.Join(root, string(vod.Rank))
	if err := os.MkdirAll(rankDir, 0o755); err != nil {
		t.Fatalf("mkdir rank dir: %v", err)
	}

	path := filepath.Join(rankDir, "diamond_example__abc123.mp4")
	if err := os.WriteFile(path, []byte("video"), 0o644); err != nil {
		t.Fatalf("write fake video: %v", err)
	}

	gotPath, gotSize, ok := FindLocalVideo(root, vod)
	if !ok {
		t.Fatal("expected local video to be found")
	}
	if gotPath != path {
		t.Fatalf("unexpected path: got %q want %q", gotPath, path)
	}
	if gotSize != 5 {
		t.Fatalf("unexpected size: got %d want 5", gotSize)
	}
}

func TestFindLocalVideoMissingReturnsExpectedMP4Path(t *testing.T) {
	root := t.TempDir()
	vod := VOD{
		Rank:    RankIron,
		Label:   "iron_example",
		VideoID: "missing",
	}

	gotPath, gotSize, ok := FindLocalVideo(root, vod)
	if ok {
		t.Fatal("expected missing local video")
	}
	if gotSize != 0 {
		t.Fatalf("unexpected size: got %d want 0", gotSize)
	}

	want := filepath.Join(root, "iron", "iron_example__missing.mp4")
	if gotPath != want {
		t.Fatalf("unexpected fallback path: got %q want %q", gotPath, want)
	}
}
