#!/usr/bin/env bash
set -euo pipefail

HOST="${HOST:-127.0.0.1}"
PORT="${PORT:-8091}"
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

cd "$ROOT"
export PYTHONPATH="$ROOT/ml/vision-service${PYTHONPATH:+:$PYTHONPATH}"
exec python3 -m app.server --host "$HOST" --port "$PORT"
