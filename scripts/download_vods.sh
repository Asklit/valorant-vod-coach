#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"

MANIFEST="${MANIFEST:-${ROOT_DIR}/data/manifests/vods.tsv}"
OUT_ROOT="${OUT_ROOT:-${ROOT_DIR}/data/raw/youtube}"
QUALITY="${QUALITY:-1080}"
YOUTUBE_PLAYER_CLIENTS="${YOUTUBE_PLAYER_CLIENTS:-android,ios,web_embedded}"
RANK_FILTER=""
PRINT_ONLY=0

usage() {
  cat <<'USAGE'
Usage:
  scripts/download_vods.sh [--rank RANK] [--print-only] [--manifest PATH] [--out-dir PATH]

Environment:
  QUALITY=1080          Max video height. Default: 1080.
  YOUTUBE_PLAYER_CLIENTS=android,ios,web_embedded
                        YouTube clients used by yt-dlp. Default avoids some 403s.
  YTDLP_EXTRA_ARGS=""   Extra args passed to yt-dlp.

Examples:
  scripts/download_vods.sh --print-only
  scripts/download_vods.sh --rank diamond
  QUALITY=720 scripts/download_vods.sh
USAGE
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --rank)
      RANK_FILTER="${2:-}"
      shift 2
      ;;
    --print-only)
      PRINT_ONLY=1
      shift
      ;;
    --manifest)
      MANIFEST="${2:-}"
      shift 2
      ;;
    --out-dir)
      OUT_ROOT="${2:-}"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "Unknown argument: $1" >&2
      usage >&2
      exit 2
      ;;
  esac
done

if [[ ! -f "$MANIFEST" ]]; then
  echo "Manifest not found: $MANIFEST" >&2
  exit 1
fi

if [[ "$PRINT_ONLY" -eq 0 ]]; then
  command -v yt-dlp >/dev/null 2>&1 || {
    echo "yt-dlp is required. Install it with: brew install yt-dlp" >&2
    exit 1
  }
  command -v ffmpeg >/dev/null 2>&1 || {
    echo "ffmpeg is required. Install it with: brew install ffmpeg" >&2
    exit 1
  }
fi

FORMAT="bv*[height<=${QUALITY}][fps<=60][ext=mp4]+ba[ext=m4a]/b[height<=${QUALITY}][ext=mp4]/bv*[height<=${QUALITY}][fps<=60]+ba/b[height<=${QUALITY}]"
ARCHIVE_FILE="${OUT_ROOT}/.downloaded.txt"
TMP_DIR="${OUT_ROOT}/.tmp"

mkdir -p "$OUT_ROOT" "$TMP_DIR"

count=0

while IFS=$'\t' read -r enabled rank label video_id url duration title channel rank_source notes || [[ -n "${enabled:-}" ]]; do
  [[ -z "${enabled:-}" || "${enabled:0:1}" == "#" ]] && continue
  [[ "$enabled" == "1" ]] || continue
  [[ -z "$RANK_FILTER" || "$rank" == "$RANK_FILTER" ]] || continue

  count=$((count + 1))
  rank_dir="${OUT_ROOT}/${rank}"
  output_template="${rank}/${label}__${video_id}.%(ext)s"

  if [[ "$PRINT_ONLY" -eq 1 ]]; then
    printf '%s\t%s\t%s\t%s\t%s\n' "$rank" "$duration" "$video_id" "$title" "$url"
    continue
  fi

  mkdir -p "$rank_dir"
  echo "Downloading [$rank] $label ($duration): $title"

  # shellcheck disable=SC2086
  yt-dlp \
    --no-playlist \
    --continue \
    --retries 10 \
    --fragment-retries 10 \
    --retry-sleep "http:exp=1:20" \
    --retry-sleep "fragment:exp=1:20" \
    --extractor-args "youtube:player_client=${YOUTUBE_PLAYER_CLIENTS}" \
    --format "$FORMAT" \
    --merge-output-format mp4 \
    --recode-video mp4 \
    --write-info-json \
    --download-archive "$ARCHIVE_FILE" \
    --paths "$OUT_ROOT" \
    --paths "temp:${TMP_DIR}" \
    --output "$output_template" \
    ${YTDLP_EXTRA_ARGS:-} \
    "$url"
done < "$MANIFEST"

if [[ "$count" -eq 0 ]]; then
  if [[ -n "$RANK_FILTER" ]]; then
    echo "No enabled VODs found for rank: $RANK_FILTER" >&2
  else
    echo "No enabled VODs found in manifest: $MANIFEST" >&2
  fi
  exit 1
fi
