package media

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

const ContactSheetName = "contact_sheet.jpg"

type ContactSheetOptions struct {
	FFmpegPath string
	FramesDir  string
	OutputPath string
	Columns    int
	Rows       int
	TileWidth  int
	TileHeight int
	Padding    int
	Margin     int
	Overwrite  bool
}

type ContactSheetResult struct {
	Path    string
	Columns int
	Rows    int
}

func RunContactSheet(ctx context.Context, options ContactSheetOptions) (ContactSheetResult, error) {
	if options.FFmpegPath == "" {
		options.FFmpegPath = "ffmpeg"
	}
	if options.FramesDir == "" {
		return ContactSheetResult{}, fmt.Errorf("frames dir is required")
	}
	if options.OutputPath == "" {
		options.OutputPath = filepath.Join(options.FramesDir, ContactSheetName)
	}
	if options.Columns <= 0 {
		options.Columns = 4
	}
	if options.Rows <= 0 {
		options.Rows = 3
	}
	if options.TileWidth <= 0 {
		options.TileWidth = 320
	}
	if options.TileHeight <= 0 {
		options.TileHeight = 180
	}
	if options.Padding < 0 {
		options.Padding = 0
	}
	if options.Margin < 0 {
		options.Margin = 0
	}

	if !options.Overwrite {
		if _, err := os.Stat(options.OutputPath); err == nil {
			return ContactSheetResult{}, fmt.Errorf("contact sheet already exists: %s", options.OutputPath)
		}
	}

	framePattern := filepath.Join(options.FramesDir, "frame_%06d.jpg")
	filter := fmt.Sprintf(
		"scale=%d:%d:force_original_aspect_ratio=decrease,pad=%d:%d:(ow-iw)/2:(oh-ih)/2:color=0x10151d,tile=%dx%d:padding=%d:margin=%d:color=0x07090d",
		options.TileWidth,
		options.TileHeight,
		options.TileWidth,
		options.TileHeight,
		options.Columns,
		options.Rows,
		options.Padding,
		options.Margin,
	)

	args := []string{"-hide_banner", "-loglevel", "error"}
	if options.Overwrite {
		args = append(args, "-y")
	} else {
		args = append(args, "-n")
	}
	args = append(args,
		"-framerate", "1",
		"-i", framePattern,
		"-vf", filter,
		"-frames:v", "1",
		"-q:v", "3",
		options.OutputPath,
	)
	if err := runFFmpeg(ctx, options.FFmpegPath, args); err != nil {
		return ContactSheetResult{}, err
	}
	if _, err := os.Stat(options.OutputPath); err != nil {
		return ContactSheetResult{}, fmt.Errorf("contact sheet output missing: %w", err)
	}

	return ContactSheetResult{
		Path:    filepath.ToSlash(options.OutputPath),
		Columns: options.Columns,
		Rows:    options.Rows,
	}, nil
}
