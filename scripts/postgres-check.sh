#!/usr/bin/env bash
# Disposable container, dynamic loopback port, no persistent volume.
set -euo pipefail
cd "$(dirname "$0")/.."
container="pluto-core-check-$$"
cleanup() { docker rm -f "$container" >/dev/null 2>&1 || true; }
trap cleanup EXIT
docker info >/dev/null
docker run -d --rm --name "$container" \
  -e POSTGRES_USER=pluto_demo -e POSTGRES_PASSWORD=local_only -e POSTGRES_DB=pluto_demo \
  -p 127.0.0.1::5432 postgres:17.6-alpine >/dev/null
ready=false
for attempt in $(seq 1 40); do
  if docker exec "$container" pg_isready -U pluto_demo -d pluto_demo >/dev/null 2>&1; then
    ready=true
    break
  fi
  sleep 1
done
if [[ "$ready" != true ]]; then
  docker logs "$container"
  exit 1
fi
port=$(docker port "$container" 5432/tcp | awk -F: '{print $NF}')
PLUTO_CORE_TEST_DATABASE_URL="postgres://pluto_demo:local_only@127.0.0.1:${port}/pluto_demo?sslmode=disable" \
  sh -c 'cd stores/postgres && go test -race -count=1 ./... -run TestPostgres -v'
