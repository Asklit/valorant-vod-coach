#!/usr/bin/env bash
set -euo pipefail

max_bytes="${MAX_GIT_FILE_BYTES:-52428800}"

blocked_path_re='(^data/raw/|^data/processed/|^data/cache/|^bin/|^\.cache/)'
blocked_ext_re='\.(mp4|mkv|webm|mov|avi|flv|part|ytdl|tmp)$'

fail=0

while IFS= read -r path; do
  [[ -z "$path" ]] && continue

  if [[ "$path" =~ $blocked_path_re ]]; then
    case "$path" in
      data/raw/.gitkeep|data/raw/youtube/.gitkeep)
        ;;
      *)
        echo "blocked staged path: $path" >&2
        fail=1
        continue
        ;;
    esac
  fi

  if [[ "$path" =~ $blocked_ext_re ]]; then
    echo "blocked staged media/temp file: $path" >&2
    fail=1
    continue
  fi

  if [[ -f "$path" ]]; then
    size="$(wc -c < "$path" | tr -d ' ')"
    if [[ "$size" -gt "$max_bytes" ]]; then
      echo "blocked staged large file: $path (${size} bytes > ${max_bytes})" >&2
      fail=1
    fi
  fi
done < <(git diff --cached --name-only --diff-filter=ACMR)

if [[ "$fail" -ne 0 ]]; then
  cat >&2 <<'MSG'

Refusing to proceed because the git index contains local data, generated artifacts,
media files, or unusually large files. Remove them from the index before commit:

  git restore --staged <path>

MSG
  exit 1
fi

echo "git index ok"
