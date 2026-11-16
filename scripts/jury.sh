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
    printf 'Qwen 4B и локальный поиск на CPU. Первая загрузка: около 3,3 ГБ + 622 МБ.\n'
    docker compose -f compose.yaml -f compose.cpu.yaml up --build --wait --wait-timeout 900
    ;;
  --check) docker compose -f compose.yaml -f compose.cpu.yaml config --quiet; printf 'Конфигурация корректна.\n'; exit 0 ;;
  '') docker compose up --build --wait ;;
  *) printf 'Использование: sh scripts/jury.sh [--model|--check]\n' >&2; exit 1 ;;
esac
printf 'AI Buddy: http://127.0.0.1:%s\n' "${BUDDY_PORT:-8090}"
