#!/usr/bin/env bash
set -euo pipefail

PROJECT_DIR="${PROJECT_DIR:-/opt/auth-gate}"
ENV_FILE="/etc/auth-gate.env"
BIN="$PROJECT_DIR/bin/auth-gate"

if [ "$(id -u)" -ne 0 ]; then
  echo "install.sh must be run as root" >&2
  exit 1
fi

cd "$PROJECT_DIR"

if [ ! -x "$BIN" ]; then
  if command -v go >/dev/null 2>&1 || command -v docker >/dev/null 2>&1; then
    bash "$PROJECT_DIR/scripts/build.sh"
  else
    echo "未找到 $BIN，且本机无 go/docker。请先运行 scripts/build.sh。" >&2
    exit 1
  fi
fi

install -m 0644 systemd/auth-gate.service /etc/systemd/system/auth-gate.service

if [ ! -f "$ENV_FILE" ]; then
  read -r -s -p "Gate password: " password
  printf "\n"
  "$BIN" genenv "$password" > "$ENV_FILE"
  chmod 0600 "$ENV_FILE"
  echo "已生成 $ENV_FILE，请按需设置 GATE_COOKIE_DOMAIN（跨子域 SSO）和 GATE_TITLE。"
else
  echo "$ENV_FILE already exists; keeping it."
fi

systemctl daemon-reload
systemctl enable --now auth-gate.service

echo
echo "Installed auth-gate. 在 Caddy 中接入受保护服务（见 caddy/Caddyfile.example）："
echo "  app.example.com { import protected 127.0.0.1:<后端端口> }"
