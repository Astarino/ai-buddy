.PHONY: setup dev build test web-test coverage smoke model e2e

setup:
	go mod download
	pnpm --dir web install --frozen-lockfile

dev:
	sh scripts/dev.sh

build:
	mkdir -p tmp/bin
	go build -o tmp/bin/server ./cmd/server
	go build -o tmp/bin/corporate ./cmd/corporate
	pnpm --dir web build

test:
	TEST_DATABASE_URL="$${TEST_DATABASE_URL:-postgres:///ai_buddy_test?host=/tmp}" go test -p 1 ./cmd/... ./internal/...
	pnpm --dir web test

web-test:
	pnpm --dir web test

coverage:
	sh scripts/coverage.sh

smoke:
	python3 scripts/smoke.py

model:
	sh scripts/ollama.sh

e2e:
	pnpm --dir web build
	RUN_BROWSER_E2E=true TEST_DATABASE_URL="$${TEST_DATABASE_URL:-postgres:///ai_buddy_test?host=/tmp}" go test ./internal/e2e -run TestBrowserJourney -count=1 -v -timeout=5m
