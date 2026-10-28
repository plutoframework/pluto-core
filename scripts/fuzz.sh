#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
go test ./pagination -run '^$' -fuzz '^FuzzParse$' -fuzztime=3s -parallel=2
go test ./httpidem -run '^$' -fuzz '^FuzzKey$' -fuzztime=3s -parallel=2
