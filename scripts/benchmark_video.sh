#!/usr/bin/env bash
set -euo pipefail

MANIFEST="${MANIFEST:-data/manifests/vods.tsv}"
RAW_ROOT="${RAW_ROOT:-data/raw/youtube}"
OUT_ROOT="${OUT_ROOT:-data/processed/benchmarks}"
RUN_ID="${RUN_ID:-$(date -u +%Y%m%dT%H%M%SZ)}"

RANK_FILTER=""
LABEL_FILTER=""
LIMIT=0
FPS="1"
SAMPLE_SECONDS="180"
METADATA_ONLY=0
PRINT_ONLY=0

usage() {
  cat <<'USAGE'
Usage:
  scripts/benchmark_video.sh [options]

Options:
  --rank <rank>              Run only one rank, for example diamond.
  --label <label>            Run only one manifest label.
  --limit <n>                Stop after n selected videos.
  --fps <n>                  Frame sampling FPS. Default: 1.
  --sample-seconds <n>       Seconds to sample from each video. Default: 180.
  --metadata-only            Only run ffprobe.
  --print-only               Print selected local files without running benchmarks.
  --manifest <path>          Manifest path. Default: data/manifests/vods.tsv.
  --raw-root <path>          Raw VOD root. Default: data/raw/youtube.
  --out-root <path>          Benchmark output root. Default: data/processed/benchmarks.
  --run-id <id>              Output run ID. Default: current UTC timestamp.
  --help                     Show this help.

Examples:
  scripts/benchmark_video.sh --rank diamond --limit 1 --print-only
  scripts/benchmark_video.sh --metadata-only
  scripts/benchmark_video.sh --rank diamond --limit 1 --sample-seconds 180 --fps 1
USAGE
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --rank)
      RANK_FILTER="${2:?missing rank}"
      shift 2
      ;;
    --label)
      LABEL_FILTER="${2:?missing label}"
      shift 2
      ;;
    --limit)
      LIMIT="${2:?missing limit}"
      shift 2
      ;;
    --fps)
      FPS="${2:?missing fps}"
      shift 2
      ;;
    --sample-seconds)
      SAMPLE_SECONDS="${2:?missing sample seconds}"
      shift 2
      ;;
    --metadata-only)
      METADATA_ONLY=1
      shift
      ;;
    --print-only)
      PRINT_ONLY=1
      shift
      ;;
    --manifest)
      MANIFEST="${2:?missing manifest path}"
      shift 2
      ;;
    --raw-root)
      RAW_ROOT="${2:?missing raw root}"
      shift 2
      ;;
    --out-root)
      OUT_ROOT="${2:?missing out root}"
      shift 2
      ;;
    --run-id)
      RUN_ID="${2:?missing run ID}"
      shift 2
      ;;
    --help)
      usage
      exit 0
      ;;
    *)
      echo "Unknown option: $1" >&2
      usage >&2
      exit 2
      ;;
  esac
done

if [[ ! -f "$MANIFEST" ]]; then
  echo "Manifest not found: $MANIFEST" >&2
  exit 1
fi

command -v ffprobe >/dev/null 2>&1 || {
  echo "ffprobe is required. Install ffmpeg first." >&2
  exit 1
}

if [[ "$METADATA_ONLY" -eq 0 && "$PRINT_ONLY" -eq 0 ]]; then
  command -v ffmpeg >/dev/null 2>&1 || {
    echo "ffmpeg is required." >&2
    exit 1
  }
fi

if ! [[ "$LIMIT" =~ ^[0-9]+$ ]]; then
  echo "--limit must be a non-negative integer" >&2
  exit 2
fi

if ! [[ "$SAMPLE_SECONDS" =~ ^[0-9]+$ ]]; then
  echo "--sample-seconds must be a non-negative integer" >&2
  exit 2
fi

file_size_bytes() {
  local path="$1"
  if stat -f%z "$path" >/dev/null 2>&1; then
    stat -f%z "$path"
  else
    stat -c%s "$path"
  fi
}

dir_size_bytes() {
  local path="$1"
  if [[ ! -d "$path" ]]; then
    echo 0
    return
  fi
  du -sk "$path" | awk '{print $1 * 1024}'
}

find_video_file() {
  local rank="$1"
  local label="$2"
  local video_id="$3"
  local ext
  local candidate

  for ext in mp4 mkv webm mov; do
    candidate="$RAW_ROOT/$rank/${label}__${video_id}.${ext}"
    if [[ -f "$candidate" ]]; then
      printf '%s\n' "$candidate"
      return 0
    fi
  done

  return 1
}

run_dir="$OUT_ROOT/$RUN_ID"
results_file="$run_dir/results.tsv"

if [[ "$PRINT_ONLY" -eq 0 ]]; then
  mkdir -p "$run_dir/probes" "$run_dir/frames" "$run_dir/logs"
  if [[ ! -f "$results_file" ]]; then
    printf 'run_id\tphase\tstatus\trank\tlabel\tvideo_id\tfile_path\tfile_size_bytes\tmanifest_duration\tsample_seconds\tsample_fps\twall_seconds\tframe_count\tartifact_bytes\tnotes\n' > "$results_file"
  fi
fi

selected=0

while IFS=$'\t' read -r enabled rank label video_id url duration title channel rank_source notes; do
  if [[ "$enabled" == \#* || -z "${enabled:-}" ]]; then
    continue
  fi

  if [[ "$enabled" != "1" ]]; then
    continue
  fi

  if [[ -n "$RANK_FILTER" && "$rank" != "$RANK_FILTER" ]]; then
    continue
  fi

  if [[ -n "$LABEL_FILTER" && "$label" != "$LABEL_FILTER" ]]; then
    continue
  fi

  if [[ "$LIMIT" -gt 0 && "$selected" -ge "$LIMIT" ]]; then
    break
  fi

  if ! video_file="$(find_video_file "$rank" "$label" "$video_id")"; then
    if [[ "$PRINT_ONLY" -eq 1 ]]; then
      printf 'missing\t%s\t%s\t%s\n' "$rank" "$label" "$video_id"
    else
      printf '%s\tprobe\tmissing\t%s\t%s\t%s\t\t0\t%s\t0\t%s\t0\t0\t0\tlocal file not found\n' \
        "$RUN_ID" "$rank" "$label" "$video_id" "$duration" "$FPS" >> "$results_file"
    fi
    selected=$((selected + 1))
    continue
  fi

  if [[ "$PRINT_ONLY" -eq 1 ]]; then
    printf 'selected\t%s\t%s\t%s\t%s\n' "$rank" "$label" "$video_id" "$video_file"
    selected=$((selected + 1))
    continue
  fi

  safe_label="${label//[^a-zA-Z0-9_=-]/_}"
  probe_file="$run_dir/probes/${safe_label}.ffprobe.json"
  probe_time_file="$run_dir/logs/${safe_label}.probe.time"
  sample_time_file="$run_dir/logs/${safe_label}.sample.time"
  sample_log_file="$run_dir/logs/${safe_label}.sample.log"
  frames_dir="$run_dir/frames/${safe_label}_${FPS}fps_${SAMPLE_SECONDS}s"
  size_bytes="$(file_size_bytes "$video_file")"

  if /usr/bin/time -p ffprobe -v error -print_format json -show_format -show_streams "$video_file" > "$probe_file" 2> "$probe_time_file"; then
    probe_status="ok"
    probe_notes=""
  else
    probe_status="failed"
    probe_notes="ffprobe failed"
  fi

  probe_wall="$(awk '/^real / {print $2}' "$probe_time_file" | tail -n 1)"
  printf '%s\tprobe\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t0\t%s\t%s\t0\t%s\t%s\n' \
    "$RUN_ID" "$probe_status" "$rank" "$label" "$video_id" "$video_file" "$size_bytes" "$duration" "$FPS" "${probe_wall:-0}" "$(file_size_bytes "$probe_file")" "$probe_notes" >> "$results_file"

  if [[ "$METADATA_ONLY" -eq 1 ]]; then
    selected=$((selected + 1))
    continue
  fi

  mkdir -p "$frames_dir"
  if /usr/bin/time -p ffmpeg -hide_banner -loglevel error -y -t "$SAMPLE_SECONDS" -i "$video_file" -vf "fps=$FPS" -q:v 3 "$frames_dir/frame_%06d.jpg" > "$sample_log_file" 2> "$sample_time_file"; then
    sample_status="ok"
    sample_notes=""
  else
    sample_status="failed"
    sample_notes="ffmpeg sample failed"
  fi

  sample_wall="$(awk '/^real / {print $2}' "$sample_time_file" | tail -n 1)"
  frame_count="$(find "$frames_dir" -type f -name 'frame_*.jpg' | wc -l | tr -d ' ')"
  artifact_bytes="$(dir_size_bytes "$frames_dir")"

  printf '%s\tsample\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n' \
    "$RUN_ID" "$sample_status" "$rank" "$label" "$video_id" "$video_file" "$size_bytes" "$duration" "$SAMPLE_SECONDS" "$FPS" "${sample_wall:-0}" "$frame_count" "$artifact_bytes" "$sample_notes" >> "$results_file"

  selected=$((selected + 1))
done < "$MANIFEST"

if [[ "$PRINT_ONLY" -eq 0 ]]; then
  echo "Benchmark results: $results_file"
fi
