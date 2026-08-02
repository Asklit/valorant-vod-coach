#!/usr/bin/env bash
set -euo pipefail

base_url="${VODCOACH_BASE_URL:-http://localhost:8090}"
email="${SMOKE_EMAIL:-compose-smoke@example.com}"
password="${SMOKE_PASSWORD:-compose-smoke-password}"
name="${SMOKE_NAME:-Compose Smoke}"
bootstrap_token="${VODCOACH_BOOTSTRAP_TOKEN:-replace-with-a-random-bootstrap-token}"
cookie_jar="$(mktemp "${TMPDIR:-/tmp}/vodcoach-smoke.XXXXXX")"
response_file="$(mktemp "${TMPDIR:-/tmp}/vodcoach-response.XXXXXX")"
trap 'rm -f "$cookie_jar" "$response_file"' EXIT

request_status() {
  curl --silent --show-error --output "$response_file" --write-out '%{http_code}' "$@"
}

expect_status() {
  local expected="$1"
  shift
  local actual
  actual="$(request_status "$@")"
  if [[ "$actual" != "$expected" ]]; then
    echo "expected HTTP $expected, got $actual for $*" >&2
    cat "$response_file" >&2
    exit 1
  fi
}

expect_status 200 "$base_url/healthz"
expect_status 200 "$base_url/readyz"
expect_status 200 "$base_url/review?vod=compose-smoke"
grep -q '<div id="root"></div>' "$response_file"
expect_status 401 "$base_url/api/vods"

expect_status 200 --cookie-jar "$cookie_jar" "$base_url/api/auth/session"
if grep -q '"setup_required": true' "$response_file"; then
  auth_body="$(printf '{"email":"%s","password":"%s","display_name":"%s","setup_token":"%s"}' "$email" "$password" "$name" "$bootstrap_token")"
  expect_status 201 --cookie "$cookie_jar" --cookie-jar "$cookie_jar" --header 'Content-Type: application/json' --data "$auth_body" "$base_url/api/auth/register"
else
  auth_body="$(printf '{"email":"%s","password":"%s"}' "$email" "$password")"
  expect_status 200 --cookie "$cookie_jar" --cookie-jar "$cookie_jar" --header 'Content-Type: application/json' --data "$auth_body" "$base_url/api/auth/login"
fi

csrf_token="$(sed -n 's/.*"csrf_token": "\([^"]*\)".*/\1/p' "$response_file" | head -n 1)"
if [[ -z "$csrf_token" ]]; then
  echo "auth response does not contain a CSRF token" >&2
  cat "$response_file" >&2
  exit 1
fi

expect_status 200 --cookie "$cookie_jar" "$base_url/api/vods"
grep -q '"vods"' "$response_file"
expect_status 200 --cookie "$cookie_jar" "$base_url/api/admin/overview"
expect_status 200 --cookie "$cookie_jar" "$base_url/api/admin/telemetry?window=1h"
expect_status 200 --cookie "$cookie_jar" --header "X-CSRF-Token: $csrf_token" --request POST "$base_url/api/auth/logout"

echo "compose product smoke: ok ($base_url)"
