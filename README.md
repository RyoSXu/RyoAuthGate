# auth-gate

[English](README.md) | [简体中文](README.zh-CN.md)

A tiny unified **forward-auth** gate: password login with cross-subdomain SSO, designed to sit in front of self-hosted services that have no auth of their own. One login covers every protected subdomain.

- Single Go binary, standard library only, no runtime dependencies.
- Password login: PBKDF2-SHA256 verification, HMAC-signed session cookie, failed-attempt rate limiting.
- Session cookie set on a parent domain, so **one login covers all subdomains**.
- Works with Caddy `forward_auth` (or any proxy that supports an external auth subrequest).
- Leave apps that already have their own login untouched — just don't put them behind the gate.

## Footprint

```text
Binary size: about 6 MB
Runtime memory: about 8 MB RSS (single process)
Database: none
Dependencies: none (standard library only)
```

## How It Works

```text
Browser --> Caddy --(forward_auth /verify)--> auth-gate
                       |-- logged in  --> proxy to the backend service
                       |-- logged out --> redirect to the gate login page, then back
```

Endpoints (served by the gate):

- `/verify` — auth check for the proxy: `204` if logged in, `302` to the login page otherwise.
- `/login` — login page (GET) and form submit (POST).
- `/logout` — clears the session.
- `/health` or `/healthz` — liveness probe (`204`).

## Project Layout

```text
cmd/auth-gate/main.go        The gate (single file, standard library only)
scripts/build.sh             Unified build (local Go or Docker)
scripts/install.sh           Install + enable
scripts/update.sh            Pull, rebuild, restart
systemd/auth-gate.service    systemd unit template
caddy/Caddyfile.example      Reusable (protected) snippet + example sites
.env.example                 Example environment variables
```

## Requirements

- Linux with systemd
- A reverse proxy with external-auth support (Caddy `forward_auth` recommended)
- Go 1.22+ to build (locally, or with the Docker `golang:1-alpine` image)

## Build

```bash
bash scripts/build.sh
```

Manual build:

```bash
CGO_ENABLED=0 go build -ldflags='-s -w' -o bin/auth-gate ./cmd/auth-gate
```

## Install

```bash
git clone https://github.com/RyoSXu/RyoAuthGate.git /opt/auth-gate
cd /opt/auth-gate
sudo bash scripts/install.sh
```

The installer builds the binary (if needed), installs the systemd unit, asks for a password, and writes `/etc/auth-gate.env` (use `chmod 600`). Set `GATE_COOKIE_DOMAIN` to your parent domain for cross-subdomain SSO; `GATE_TITLE` sets the browser tab title and login page heading.

## Configuration

Generate an env file with the built-in command (no extra tooling needed):

```bash
bin/auth-gate genenv 'your-password' | sudo tee /etc/auth-gate.env
```

| Variable | Default | Description |
|---|---|---|
| `GATE_HOST` | `127.0.0.1` | Listen address |
| `GATE_PORT` | `3001` | Listen port |
| `GATE_COOKIE_NAME` | `__Secure-session` | Session cookie name |
| `GATE_COOKIE_DOMAIN` | _(empty)_ | Parent domain for cross-subdomain SSO; empty = current host only |
| `GATE_LOGIN_PATH` | `/_auth` | Public path prefix each site exposes for the gate |
| `GATE_SESSION_TTL` | `15552000` | Session lifetime in seconds (180 days) |
| `GATE_TITLE` | `Login` | Browser tab title and login page heading |
| `GATE_PASSWORD_HASH` | _(required)_ | `pbkdf2_sha256$iter$salt$hash` |
| `GATE_SECRET` | _(required)_ | HMAC signing secret |

## Caddy Integration

Define the snippet once, then `import protected <backend>` on each site (see `caddy/Caddyfile.example`):

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

## Update

```bash
cd /opt/auth-gate
bash scripts/update.sh
```

The script runs `git pull --ff-only`, calls `scripts/build.sh`, restarts the service, and checks `/health`.

## Security Notes

- Keep `GATE_SECRET` private; keep `/etc/auth-gate.env` out of Git.
- Protected backends must bind `127.0.0.1` only, so the gate cannot be bypassed.
- The session cookie is `Secure; HttpOnly; SameSite=Lax` — serve everything over HTTPS.
- Rotate the password by regenerating `/etc/auth-gate.env` (a new secret invalidates all sessions) and restarting the service.

## License

MIT
