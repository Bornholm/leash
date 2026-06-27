# Running LeaSH MCP Server with Docker

`ghcr.io/bornholm/leash-server` is a ready-to-use Docker image for the LeaSH **multi-tenant MCP HTTP Streaming server**. It bundles [bubblewrap](https://github.com/containers/bubblewrap) so each tenant's shell scripts run inside an isolated Linux namespace sandbox.

## Prerequisites

### Host kernel requirements

Bubblewrap creates Linux namespace sandboxes using `CLONE_NEWUSER` and `mount --make-rslave`. Both operations require `CAP_SYS_ADMIN`, which means the container must run with **`--privileged`** (or an equivalent capability grant).

> **Security note:** `--privileged` gives the container broad host access. LeaSH mitigates this by running the server process as an unprivileged user (`leash`, uid 1000) and by confining each tenant's shell inside a bubblewrap sandbox. Nevertheless, only run LeaSH containers on hosts you control and trust.

On most modern Linux kernels (≥ 3.18) user namespaces are enabled by default. You can verify:

```bash
cat /proc/sys/kernel/unprivileged_userns_clone   # Debian/Ubuntu — must be 1
# or
sysctl kernel.unprivileged_userns_clone           # same check
```

If the value is `0`, enable it with:

```bash
sudo sysctl -w kernel.unprivileged_userns_clone=1
# Make it persistent:
echo 'kernel.unprivileged_userns_clone=1' | sudo tee /etc/sysctl.d/99-userns.conf
```

> **Note for rootless Docker / Podman:** user namespace nesting is typically already available. You still need `--privileged` for the `mount` namespace operations.

---

## Quick start

### 1. Generate secrets

```bash
# HMAC secret — at least 32 characters, high entropy
LEASH_HMAC_SECRET=$(openssl rand -hex 32)

# API key — at least 20 characters, share this with MCP clients
LEASH_APIKEY_DEFAULT=$(openssl rand -hex 24)

echo "HMAC secret : $LEASH_HMAC_SECRET"
echo "API key     : $LEASH_APIKEY_DEFAULT"
```

Keep these values; you will need them in the next step and when configuring clients.

### 2. Create a `.env` file

```bash
cat > leash.env <<EOF
LEASH_HMAC_SECRET=${LEASH_HMAC_SECRET}
LEASH_APIKEY_DEFAULT=${LEASH_APIKEY_DEFAULT}
EOF
```

### 3. Run the container

```bash
docker run -d \
  --name leash-server \
  --env-file leash.env \
  --privileged \
  -p 127.0.0.1:8443:8443 \
  -v leash-workspaces:/var/lib/leash/workspaces \
  ghcr.io/bornholm/leash-server:latest
```

The server is now listening on `http://localhost:8443`.

Verify it is healthy:

```bash
curl http://localhost:8443/healthz
# {"status":"ok"}
```

---

## Docker Compose

Create `docker-compose.yml`:

```yaml
services:
  leash:
    image: ghcr.io/bornholm/leash-server:latest
    restart: unless-stopped
    env_file: leash.env
    privileged: true
    ports:
      - "127.0.0.1:8443:8443"
    volumes:
      - workspaces:/var/lib/leash/workspaces

volumes:
  workspaces:
```

Then:

```bash
docker compose up -d
docker compose logs -f leash
```

---

## Environment variables

All configuration is done via environment variables (or `.env` file in the current directory).

### Required

| Variable | Description |
|---|---|
| `LEASH_HMAC_SECRET` | ≥ 32-char secret used to HMAC-derive workspace directory names. Never expose this. |
| `LEASH_APIKEY_<NAME>` | Bearer token for API key `<NAME>`. At least one is required. |

### Optional — keys and workspaces

| Variable | Default | Description |
|---|---|---|
| `LEASH_APIKEY_<NAME>_WORKSPACE_ID` | _(discriminant)_ | Pin all requests from this key to a fixed workspace, ignoring the `X-Workspace` header. |
| `LEASH_APIKEY_<NAME>_ENV` | _(empty)_ | Extra env vars injected into this key's sandbox, e.g. `FOO=bar,BAZ=qux`. |
| `LEASH_APIKEY_<NAME>_POLICY` | _(hardened defaults)_ | Path to a per-key [policy file](policies.md). Controls allowed binaries, builtins, MCP sub-servers, and sandbox settings. |
| `LEASH_WORKSPACE_ROOT` | `/var/lib/leash/workspaces` | Directory under which per-tenant workspace subdirectories are created. |
| `LEASH_TTL` | `30m` | Inactivity TTL before a workspace is reaped (Go duration syntax). |

### Optional — networking

| Variable | Default | Description |
|---|---|---|
| `LEASH_MCP_LISTEN_ADDR` | `0.0.0.0:8443` | TCP address the HTTP server binds to. |
| `LEASH_DISC_HEADER` | `X-Workspace` | HTTP header carrying the tenant discriminant. |
| `LEASH_DISC_URL_PARAM` | `workspace` | URL query parameter carrying the tenant discriminant (fallback if header is absent). |
| `LEASH_TRUST_PROXY_HEADERS` | `false` | Set to `true` when behind a reverse proxy — enables `X-Forwarded-For` / `X-Real-IP` for rate limiting. |

### Optional — rate limiting

| Variable | Default | Description |
|---|---|---|
| `LEASH_HTTP_RATE_LIMIT` | `60/minute` | Per-IP token-bucket rate (`N/minute` or `N/second`). |
| `LEASH_HTTP_BURST` | `20` | Maximum burst size for the per-IP limiter. |
| `LEASH_MAX_WORKSPACES` | `100` | Maximum number of concurrent live workspaces. |
| `LEASH_MAX_REQUEST_BODY_BYTES` | `1048576` | Maximum request body size in bytes (1 MiB). |

### Optional — observability

| Variable | Default | Description |
|---|---|---|
| `LEASH_LOG_LEVEL` | `info` | Log verbosity: `debug`, `info`, `warn`, `error`. |

---

## Connecting an MCP client

LeaSH uses the [MCP Streamable HTTP transport](https://modelcontextprotocol.io/specification/2025-11-25/basic/transports#streamable-http). Configure your client to POST to the server's base URL with `Authorization: Bearer <API_KEY>`.

### Claude Desktop

In `claude_desktop_config.json`:

```json
{
  "mcpServers": {
    "leash": {
      "type": "http",
      "url": "http://localhost:8443",
      "headers": {
        "Authorization": "Bearer <LEASH_APIKEY_DEFAULT value>",
        "X-Workspace": "my-project"
      }
    }
  }
}
```

### Cursor / VS Code (MCP extension)

```json
{
  "mcp": {
    "servers": {
      "leash": {
        "type": "http",
        "url": "http://localhost:8443",
        "headers": {
          "Authorization": "Bearer <LEASH_APIKEY_DEFAULT value>",
          "X-Workspace": "my-project"
        }
      }
    }
  }
}
```

The `X-Workspace` header is the **discriminant** that isolates tenants. Each unique value gets its own sandbox directory. Omitting it will cause the server to return a `400` (empty discriminant is rejected).

---

## Multi-tenant setup

You can issue multiple API keys, each with its own policy and workspace:

```bash
# leash.env
LEASH_HMAC_SECRET=<long-random-secret>

# First tenant — hardened defaults (no binaries, no builtins)
LEASH_APIKEY_ALICE=<alice-token>
LEASH_APIKEY_ALICE_WORKSPACE_ID=alice   # fixed workspace, no header needed

# Second tenant — custom policy allowing specific binaries
LEASH_APIKEY_BOB=<bob-token>
LEASH_APIKEY_BOB_POLICY=/app/policies/bob.yaml
```

Each key maps to a completely isolated workspace directory (derived via HMAC-SHA256). Even if both clients send the same `X-Workspace` header value, their directories are different because the HMAC key is the same but the discriminant could differ.

---

## Behind a reverse proxy (recommended)

The server never terminates TLS; place it behind nginx, Caddy, or Traefik.

Example nginx snippet:

```nginx
server {
    listen 443 ssl;
    server_name leash.example.com;

    ssl_certificate     /etc/letsencrypt/live/leash.example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/leash.example.com/privkey.pem;

    location / {
        proxy_pass         http://127.0.0.1:8443;
        proxy_set_header   X-Real-IP $remote_addr;
        proxy_set_header   X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header   Host $host;

        # Required for MCP Streamable HTTP (SSE responses have no fixed length)
        proxy_buffering    off;
        proxy_read_timeout 3600s;
    }
}
```

Set `LEASH_TRUST_PROXY_HEADERS=true` so the per-IP rate limiter sees real client IPs instead of the proxy address.

---

## Building the image locally

```bash
# From the repository root:
docker build -f Dockerfile.server -t leash-server:local .

# Run the locally built image:
docker run --rm \
  --env-file leash.env \
  --security-opt seccomp=unconfined \
  -p 127.0.0.1:8443:8443 \
  leash-server:local
```

---

## Troubleshooting

### `bwrap: creating new namespace: Operation not permitted`

User namespaces are disabled on the host or blocked by the seccomp profile. Check:

```bash
# Is the kernel option enabled?
sysctl kernel.unprivileged_userns_clone   # must be 1

# Did you pass --security-opt seccomp=unconfined?
docker inspect leash-server | grep -i seccomp
```

### `401 Unauthorized`

- The `Authorization: Bearer …` header is missing or the token does not match any configured `LEASH_APIKEY_*` variable.
- Tokens are compared against their SHA-256 hash; make sure you copy the raw value exactly.

### `400 Bad Request` on all requests

- The `X-Workspace` discriminant header is missing.
- Or the request body exceeds `LEASH_MAX_REQUEST_BODY_BYTES`.

### `429 Too Many Requests`

The per-IP rate limit was hit. Reduce request frequency or increase `LEASH_HTTP_RATE_LIMIT` / `LEASH_HTTP_BURST`.

### Workspace directory is empty after execution

The shell script ran inside the bubblewrap sandbox and wrote files to `/work`. That directory maps to the workspace directory on the host, which is mounted at `LEASH_WORKSPACE_ROOT/<hash>/`. Inspect it with:

```bash
docker exec leash-server ls /var/lib/leash/workspaces/
```
