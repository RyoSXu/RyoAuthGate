#!/usr/bin/env bash
set -euo pipefail

PROJECT_DIR="${PROJECT_DIR:-/opt/auth-gate}"
BIN="${BIN:-$PROJECT_DIR/bin/auth-gate}"
LDFLAGS='-s -w'

cd "$PROJECT_DIR"

if command -v go >/dev/null 2>&1; then
  echo "Building auth-gate ..."
  CGO_ENABLED=0 go build -ldflags="$LDFLAGS" -o "$BIN" ./cmd/auth-gate
else
  echo "Building auth-gate via Docker ..."
  docker run --rm -v "$PROJECT_DIR":/src -w /src golang:1-alpine \
    sh -c "CGO_ENABLED=0 go build -ldflags='$LDFLAGS' -o bin/auth-gate ./cmd/auth-gate"
fi
