#!/usr/bin/env sh
set -eu
cd "$(dirname "$0")/.."
if [ -f .env ]; then
  set -a
  . ./.env
  set +a
fi
export OLLAMA_HOST=127.0.0.1:11434
export OLLAMA_NO_CLOUD=1
export OLLAMA_NUM_PARALLEL=1
export OLLAMA_MAX_LOADED_MODELS=1
export OLLAMA_CONTEXT_LENGTH=4096
export OLLAMA_KEEP_ALIVE=0
export OLLAMA_MAX_QUEUE=2
export OLLAMA_FLASH_ATTENTION=1
export OLLAMA_KV_CACHE_TYPE=q8_0
if [ "$(uname -s)" != Darwin ]; then
  exec "${OLLAMA_BIN:-ollama}" serve
fi

minimum_free=${OLLAMA_MIN_FREE_MEMORY_PERCENT:-25}
case "$minimum_free" in
  ''|*[!0-9]*) printf 'OLLAMA_MIN_FREE_MEMORY_PERCENT must be an integer from 1 to 99\n' >&2; exit 1 ;;
esac
if [ "$minimum_free" -lt 1 ] || [ "$minimum_free" -gt 99 ]; then
  printf 'OLLAMA_MIN_FREE_MEMORY_PERCENT must be from 1 to 99\n' >&2
  exit 1
fi
"${OLLAMA_BIN:-ollama}" serve &
runtime_pid=$!
cleanup() {
  kill "$runtime_pid" 2>/dev/null || true
  wait "$runtime_pid" 2>/dev/null || true
}
trap cleanup EXIT
trap 'exit 130' INT TERM
while kill -0 "$runtime_pid" 2>/dev/null; do
  free_percent=$(LC_ALL=C /usr/bin/memory_pressure | sed -n 's/.*memory free percentage: \([0-9]*\)%.*/\1/p')
  case "$free_percent" in
    ''|*[!0-9]*) printf 'Cannot read memory pressure; stopping owned Ollama runtime\n' >&2; exit 1 ;;
  esac
  if [ "$free_percent" -lt "$minimum_free" ]; then
    printf 'Free memory %s%% is below %s%%; stopping owned Ollama runtime\n' "$free_percent" "$minimum_free" >&2
    exit 1
  fi
  sleep 1
done
wait "$runtime_pid"
