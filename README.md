# auth-gate

轻量的统一 forward-auth 网关（密码登录，跨子域 SSO），配合 Caddy / Traefik 等反向代理使用。

- 单个 Go 二进制，仅标准库，无第三方依赖（~6MB）。
- 密码登录：PBKDF2-sha256 校验，HMAC 签名的会话 Cookie，失败限流。
- 会话 Cookie 设在父域（如 `ryoxu.me`），**一次登录覆盖所有子域**的受保护服务。
- 端点：`/verify`（forward-auth 判定，已登录 204 / 未登录 302 跳登录页）、`/login`、`/logout`、`/health`。

## 构建

```bash
CGO_ENABLED=0 go build -ldflags='-s -w' -o bin/auth-gate ./cmd/auth-gate
# 或用 Docker：
docker run --rm -v "$PWD":/src -w /src golang:1-alpine \
  sh -c "CGO_ENABLED=0 go build -ldflags='-s -w' -o bin/auth-gate ./cmd/auth-gate"
```

## 配置

用内置命令生成环境变量文件（含密码哈希与随机 secret）：

```bash
bin/auth-gate genenv '你的密码' | sudo tee /etc/auth-gate.env
```

主要变量：`GATE_PORT`、`GATE_COOKIE_NAME`、`GATE_COOKIE_DOMAIN`、`GATE_LOGIN_PATH`、`GATE_SESSION_TTL`、`GATE_PASSWORD_HASH`、`GATE_SECRET`。

## Caddy 接入

```caddyfile
(protected) {
    handle_path /_auth/* {
        reverse_proxy 127.0.0.1:3001
    }
    handle {
        forward_auth 127.0.0.1:3001 {
            uri /verify
            header_up X-Original-URI {uri}
        }
        reverse_proxy {args[0]}
    }
}

app.example.com {
    import protected 127.0.0.1:8080
}
```
