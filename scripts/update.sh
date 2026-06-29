#!/usr/bin/env bash
set -euo pipefail

PROJECT_DIR="${PROJECT_DIR:-/opt/auth-gate}"
BIN="$PROJECT_DIR/bin/auth-gate"

cd "$PROJECT_DIR"

if command -v git >/dev/null 2>&1 && [ -d .git ]; then
  git pull --ff-only
fi

bash "$PROJECT_DIR/scripts/build.sh"

install -m 0644 systemd/auth-gate.service /etc/systemd/system/auth-gate.service
systemctl daemon-reload
systemctl restart auth-gate.service
systemctl is-active --quiet auth-gate.service

for _ in $(seq 1 10); do
  if curl -fsS --max-time 5 http://127.0.0.1:3001/health >/dev/null; then
    echo "auth-gate updated successfully."
    exit 0
  fi
  sleep 1
done

echo "auth-gate health check failed." >&2
exit 1
