#!/usr/bin/env sh
set -eu
cd "$(dirname "$0")/.."
if ! command -v docker >/dev/null 2>&1; then
  printf 'Нужен Docker с Compose. Нативный способ описан в docs/run.md.\n' >&2
  exit 1
fi
docker compose version >/dev/null
case "${1:-}" in
  --model)
    printf 'Настоящая Qwen 4B на CPU. Первый запуск загружает около 4 ГБ весов.\n'
    docker compose -f compose.yaml -f compose.cpu.yaml up --build --wait --wait-timeout 900
    ;;
  --check) docker compose -f compose.yaml -f compose.cpu.yaml config --quiet ;;
  '') docker compose up --build --wait ;;
  *) printf 'Использование: sh scripts/jury.sh [--model|--check]\n' >&2; exit 1 ;;
esac
printf 'AI Buddy: http://127.0.0.1:%s\n' "${BUDDY_PORT:-8090}"
