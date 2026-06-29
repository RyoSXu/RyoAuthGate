# auth-gate

[English](README.md) | [简体中文](README.zh-CN.md)

一个极小的统一 **forward-auth** 网关：密码登录 + 跨子域 SSO，专门放在那些「自身没有登录功能」的自托管服务前面。**一次登录，覆盖所有受保护的子域**。

- 单个 Go 二进制，纯标准库，无运行时依赖。
- 密码登录：PBKDF2-SHA256 校验、HMAC 签名的会话 Cookie、失败次数限流。
- 会话 Cookie 设在父域，**一次登录覆盖所有子域**。
- 配合 Caddy `forward_auth`（或任何支持外部鉴权子请求的反代）使用。
- 已自带登录的服务保持原样、不要放到网关后面即可。

## 占用

```text
二进制大小：约 6 MB
运行内存：约 8 MB RSS（单进程）
数据库：无
依赖：无（仅标准库）
```

## 工作方式

```text
浏览器 --> Caddy --(forward_auth /verify)--> auth-gate
                     |-- 已登录 --> 回源到后端服务
                     |-- 未登录 --> 跳到网关登录页，登录后回跳
```

网关端点：

- `/verify` —— 给反代的鉴权判定：已登录 `204`，未登录 `302` 跳登录页。
- `/login` —— 登录页（GET）与表单提交（POST）。
- `/logout` —— 清除会话。
- `/health` 或 `/healthz` —— 存活探针（`204`）。

## 目录结构

```text
cmd/auth-gate/main.go        网关本体（单文件，纯标准库）
scripts/build.sh             统一构建（本机 Go 或 Docker）
scripts/install.sh           安装 + 启用
scripts/update.sh            拉取、构建、重启
systemd/auth-gate.service    systemd 服务模板
caddy/Caddyfile.example      可复用的 (protected) 片段 + 示例站点
.env.example                 环境变量示例
```

## 运行要求

- 使用 systemd 的 Linux
- 支持外部鉴权的反向代理（推荐 Caddy `forward_auth`）
- 构建需 Go 1.22+（本机安装，或用 Docker `golang:1-alpine`）

## 构建

```bash
bash scripts/build.sh
```

手动构建：

```bash
CGO_ENABLED=0 go build -ldflags='-s -w' -o bin/auth-gate ./cmd/auth-gate
```

## 安装

```bash
git clone https://github.com/RyoSXu/RyoAuthGate.git /opt/auth-gate
cd /opt/auth-gate
sudo bash scripts/install.sh
```

安装脚本会（按需）构建二进制、安装 systemd 单元、询问密码并写入 `/etc/auth-gate.env`（建议 `chmod 600`）。跨子域 SSO 请把 `GATE_COOKIE_DOMAIN` 设为你的父域；`GATE_TITLE` 用于浏览器标签页标题与登录页品牌。

## 配置

用内置命令生成环境变量文件（无需额外工具）：

```bash
bin/auth-gate genenv '你的密码' | sudo tee /etc/auth-gate.env
```

| 变量 | 默认值 | 说明 |
|---|---|---|
| `GATE_HOST` | `127.0.0.1` | 监听地址 |
| `GATE_PORT` | `3001` | 监听端口 |
| `GATE_COOKIE_NAME` | `__Secure-session` | 会话 Cookie 名 |
| `GATE_COOKIE_DOMAIN` | _(空)_ | 跨子域 SSO 的父域；留空则仅当前主机 |
| `GATE_LOGIN_PATH` | `/_auth` | 各站点暴露给网关的公共路径前缀 |
| `GATE_SESSION_TTL` | `15552000` | 会话有效期（秒，默认 180 天） |
| `GATE_TITLE` | `Login` | 浏览器标签页标题与登录页品牌 |
| `GATE_PASSWORD_HASH` | _(必填)_ | `pbkdf2_sha256$迭代$盐$哈希` |
| `GATE_SECRET` | _(必填)_ | HMAC 签名密钥 |

## Caddy 接入

片段定义一次，之后每个站点 `import protected <后端>` 即可（见 `caddy/Caddyfile.example`）：

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

**PWA 图标免登录**：在 `import protected` 前加 `@public_icons` 块，让 `manifest` / `favicon` 等静态资源不经鉴权（完整示例见 `caddy/Caddyfile.example`）。

**剥离 Authorization**：若后端对 `Authorization` 头敏感（如 PairDrop），用 `(protected-drop)` 片段，行为同 `(protected)` 但回源时去掉该头。

## 更新

```bash
cd /opt/auth-gate
bash scripts/update.sh
```

脚本会 `git pull --ff-only`、调用 `scripts/build.sh` 重建、重启服务并检查 `/health`。

## 安全建议

- 不要泄露 `GATE_SECRET`；不要把 `/etc/auth-gate.env` 提交到 Git。
- 受保护的后端只绑 `127.0.0.1`，避免绕过网关直连。
- 会话 Cookie 为 `Secure; HttpOnly; SameSite=Lax`，请全程 HTTPS。
- 轮换密码：重新生成 `/etc/auth-gate.env`（新 secret 会让所有会话失效）并重启服务。

## 许可

MIT
