#!/usr/bin/env sh
set -eu
cd "$(dirname "$0")/.."
export TEST_DATABASE_URL="${TEST_DATABASE_URL:-postgres:///ai_buddy_test?host=/tmp}"
mkdir -p coverage
go test -p 1 -race -covermode=atomic -coverpkg=./... -coverprofile=coverage/go.out ./...
go tool cover -html=coverage/go.out -o coverage/go.html
go run github.com/jandelgado/gcov2lcov@v1.1.1 -infile=coverage/go.out -outfile=coverage/go.lcov
pnpm --dir web test:coverage
python3 scripts/coverage_report.py
