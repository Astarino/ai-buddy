#!/usr/bin/env sh
set -eu
cd "$(dirname "$0")/.."
if [ -f .env ]; then
  set -a
  . ./.env
  set +a
fi
: "${DATABASE_URL:=postgres:///ai_buddy?host=/tmp}"
: "${CORPORATE_TOKEN:=local-demo-service}"
: "${DEMO_OPERATOR_TOKEN:=local-demo-owner}"
export DATABASE_URL CORPORATE_TOKEN DEMO_OPERATOR_TOKEN
mkdir -p tmp/bin
go build -o tmp/bin/corporate ./cmd/corporate
go build -o tmp/bin/server ./cmd/server
pnpm --dir web build
tmp/bin/corporate &
corporate_pid=$!
tmp/bin/server &
server_pid=$!
cleanup() {
  kill "$server_pid" "$corporate_pid" 2>/dev/null || true
  wait "$server_pid" "$corporate_pid" 2>/dev/null || true
}
trap cleanup EXIT INT TERM
printf '\nAI Buddy: http://127.0.0.1:8090\n'
wait "$server_pid"
