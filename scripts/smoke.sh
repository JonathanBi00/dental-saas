#!/bin/bash
# End-to-end smoke test against the dev Docker services (`task up`).
# Builds dentalctl, checks migrations go up, fully down and up again, seeds twice to
# prove it's idempotent, then starts the API and signs in as the demo users.
# Used by CI; safe to run locally, but it resets the dev database.
set -euo pipefail

cd "$(dirname "$0")/../backend"

export DATABASE_URL="${DATABASE_URL:-postgres://dental_app:dental_app@localhost:5433/dental?sslmode=disable}"
export MIGRATE_DATABASE_URL="${MIGRATE_DATABASE_URL:-postgres://dental_owner:dental_owner@localhost:5433/dental?sslmode=disable}"
export HTTP_ADDR="${HTTP_ADDR:-127.0.0.1:8080}"
export APP_ORIGIN="${APP_ORIGIN:-http://localhost:5173}"
export SMTP_HOST="${SMTP_HOST:-localhost}" SMTP_PORT="${SMTP_PORT:-1025}"
export MAIL_FROM="${MAIL_FROM:-Dental SaaS <no-reply@dental.local>}"

api="http://${HTTP_ADDR}"
work="$(mktemp -d)"
bin="$work/dentalctl"
jar="$work/cookies.txt"
log="${SMOKE_LOG:-$work/api.log}"
pid=""
cleanup() {
  [ -n "$pid" ] && kill "$pid" 2>/dev/null && wait "$pid" 2>/dev/null
  rm -rf -- "$work"
}
trap cleanup EXIT

step() { printf '\n==> %s\n' "$*"; }
fail() {
  echo "FAIL: $*" >&2
  [ -s "$log" ] && { echo "--- API log (last 50 lines) ---" >&2; tail -n 50 "$log" >&2; }
  exit 1
}

step "build"
go build -o "$bin" ./cmd/dentalctl

step "migrate up / reset / up"
"$bin" migrate up
"$bin" migrate reset
"$bin" migrate up
"$bin" migrate status

step "seed twice"
"$bin" seed
"$bin" seed

step "serve"
"$bin" serve >"$log" 2>&1 &
pid=$!
for _ in $(seq 1 50); do
  curl -fsS "$api/healthz" >/dev/null 2>&1 && break
  kill -0 "$pid" 2>/dev/null || fail "API exited during startup"
  sleep 0.2
done
curl -fsS "$api/healthz" >/dev/null || fail "/healthz never became healthy"
spec=$(curl -fsS "$api/api/v1/openapi.yaml") || fail "OpenAPI spec not served"
grep -q '^openapi:' <<<"$spec" || fail "OpenAPI spec looks wrong"

# login EMAIL: signs in and stores the session cookie in $jar
login() {
  local code
  code=$(curl -sS -o /dev/null -w '%{http_code}' -c "$jar" -H 'Content-Type: application/json' \
    -d "{\"email\":\"$1\",\"password\":\"demo-password-123\"}" "$api/api/v1/auth/login")
  [ "$code" = 200 ] || fail "login as $1 returned $code"
}

# location_count: number of locations visible to the signed-in user
location_count() {
  curl -fsS -b "$jar" "$api/api/v1/locations" | python3 -c 'import json,sys; print(len(json.load(sys.stdin)["items"]))'
}

step "owner sees every location"
login owner@demo.local
owner_locations=$(location_count)
[ "$owner_locations" -ge 2 ] || fail "owner sees $owner_locations locations, want >= 2"

step "front desk is limited to one location"
login frontdesk@demo.local
[ "$(location_count)" = 1 ] || fail "front desk should see exactly 1 location"

step "unauthenticated requests are rejected"
code=$(curl -sS -o /dev/null -w '%{http_code}' "$api/api/v1/locations")
[ "$code" = 401 ] || fail "anonymous /locations returned $code, want 401"

step "cross-origin writes are rejected"
code=$(curl -sS -o /dev/null -w '%{http_code}' -H 'Origin: https://evil.example' \
  -H 'Content-Type: application/json' -d '{}' "$api/api/v1/auth/login")
[ "$code" = 403 ] || fail "cross-origin login returned $code, want 403"

echo
echo "smoke test passed"
