#!/usr/bin/env bash
# Real standalone Valkey and Redis, isolated containers, no persisted data.
set -euo pipefail
cd "$(dirname "$0")/.."
valkey="pluto-cache-valkey-$$"
redis="pluto-cache-redis-$$"
cleanup() {
  docker rm -f "$valkey" "$redis" >/dev/null 2>&1 || true
}
trap cleanup EXIT
docker run -d --rm --name "$valkey" -p 127.0.0.1::6379 \
  valkey/valkey:9.1.3-alpine valkey-server --save '' --appendonly no \
  --requirepass synthetic_only >/dev/null
docker run -d --rm --name "$redis" -p 127.0.0.1::6379 \
  redis:8.10.3-alpine redis-server --save '' --appendonly no \
  --requirepass synthetic_only >/dev/null
for container in "$valkey" "$redis"; do
  ready=false
  for attempt in $(seq 1 40); do
    if docker exec "$container" sh -c 'REDISCLI_AUTH=synthetic_only redis-cli ping 2>/dev/null || VALKEYCLI_AUTH=synthetic_only valkey-cli ping 2>/dev/null' | rg -q PONG; then
      ready=true
      break
    fi
    sleep 1
  done
  if [[ "$ready" != true ]]; then exit 1; fi
done
valkey_port=$(docker port "$valkey" 6379/tcp | awk -F: '{print $NF}')
redis_port=$(docker port "$redis" 6379/tcp | awk -F: '{print $NF}')
PLUTO_CORE_CACHE_TEST_URLS="redis://:synthetic_only@127.0.0.1:${valkey_port},redis://:synthetic_only@127.0.0.1:${redis_port}" \
  go test -race -count=1 ./stores/valkey/... -v
