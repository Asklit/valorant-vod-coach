package video

import (
	"testing"
	"time"
)

func TestParseMetadata(t *testing.T) {
	raw := []byte(`{
  "streams": [
    {
      "index": 0,
      "codec_name": "h264",
      "codec_type": "video",
      "width": 1920,
      "height": 1080,
      "avg_frame_rate": "60000/1001"
    },
    {
      "index": 1,
      "codec_name": "aac",
      "codec_type": "audio"
    }
  ],
  "format": {
    "filename": "vod.mp4",
    "nb_streams": 2,
    "format_name": "mov,mp4,m4a,3gp,3g2,mj2",
    "duration": "2224.250000",
    "size": "1301252227",
    "bit_rate": "4680312"
  }
}`)

	metadata, err := ParseMetadata(raw)
	if err != nil {
		t.Fatalf("ParseMetadata returned error: %v", err)
	}

	videoStream, ok := VideoStream(metadata)
	if !ok {
		t.Fatal("expected video stream")
	}

	if got := Resolution(videoStream); got != "1920x1080" {
		t.Fatalf("unexpected resolution: %q", got)
	}

	if got := FrameRate(videoStream); got != "59.94 fps" {
		t.Fatalf("unexpected frame rate: %q", got)
	}

	if duration, ok := Duration(metadata); !ok || duration != 2224*time.Second+250*time.Millisecond {
		t.Fatalf("unexpected duration: %v ok=%v", duration, ok)
	}

	if size, ok := SizeBytes(metadata); !ok || size != 1301252227 {
		t.Fatalf("unexpected size: %d ok=%v", size, ok)
	}
}
