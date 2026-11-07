#!/usr/bin/env sh
set -eu
repo_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
GOCACHE=${GOCACHE:-${TMPDIR:-/tmp}/bcc-fx-go-cache}
export GOCACHE
cd "$repo_dir/backend"
go test ./...
go vet ./...
go run ./cmd/eval
