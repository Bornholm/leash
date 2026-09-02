# MCP HTTP Streaming server (multi-tenant)

`server` is a standalone binary exposing LeaSH as a **multi-tenant** MCP server over the [Streamable HTTP transport](https://modelcontextprotocol.io/specification/2025-11-25/basic/transports#streamable-http). Each tenant gets its own isolated workspace directory and its own bubblewrap sandbox; workspaces are created lazily and reaped after a configurable inactivity TTL.

## Build & run

```bash
make build-server
LEASH_HMAC_SECRET=... LEASH_APIKEY_DEFAULT=... ./bin/server
```

or directly:

```bash
go build -o server ./cmd/server
LEASH_HMAC_SECRET=... LEASH_APIKEY_DEFAULT=... ./server
```

`server` loads a `.env` file from the current directory (if present) via [godotenv](https://github.com/joho/godotenv), then reads configuration from the process environment. Real environment variables always take precedence over the `.env` file.

## Authentication — Bearer only

The server authenticates **exclusively** via the standard HTTP `Authorization: Bearer <API_KEY>` header (RFC 6750):

```
Authorization: Bearer <your-api-key>
```

- The `Bearer` scheme is case-insensitive; the token is trimmed.
- There is **no** other authentication mechanism: no `X-API-Key` header, no `?api_key=` query parameter. A token in a query string would leak into access logs and browser history, so it is deliberately never supported.
- Missing or malformed `Authorization` → `401` with a `WWW-Authenticate: Bearer realm="leash-mcp"` header.
- Unknown API key → `401` with `WWW-Authenticate: Bearer realm="leash-mcp", error="invalid_token"`.
- API keys are compared in constant time against a SHA-256 hash; the raw key is never persisted in memory beyond the parsing step.

## Workspace model

Each request carries a **discriminant** that identifies the tenant — either an HTTP header or a URL variable (both configurable, see below). The discriminant is **never** used directly as a directory name: it is always passed through HMAC-SHA256 (keyed with `LEASH_HMAC_SECRET`), and the resulting 64-character hex digest becomes the workspace directory name under `LEASH_WORKSPACE_ROOT`. This guarantees that no path-traversal sequence in the discriminant can ever escape the workspace root.

If the matched API key has a `WORKSPACE_ID` override configured, it takes precedence over any header/URL discriminant — all requests authenticated with that key always land in the same fixed workspace.

Workspaces are:

- Created lazily on first use, with bubblewrap sandboxing enabled, network disabled, only the workspace directory mounted read-write, and all builtins disabled by default.
- Serialized: concurrent requests to the same workspace are executed one at a time (the underlying `leash.Engine` is not concurrency-safe).
- Reaped after `LEASH_TTL` of inactivity: the directory is removed and the engine is unloaded.

## Per-key policy

By default every API key shares the same hardened defaults: bubblewrap sandbox with network disabled and all builtins disabled. A key can opt into its own [policy file](policies.md) via `LEASH_APIKEY_<NAME>_POLICY=<path>`, which controls:

- `allowed_binaries`, `blocked_patterns`, rate limits, execution limits — same as any LeaSH policy.
- `builtins.enabled` / builtins registered programmatically — no longer force-disabled for that key.
- `mcp_servers` — external MCP servers bridged into the shell as skills for that tenant.
- its own `sandbox:` section — e.g. to allow network access so the workspace can actually reach a remote MCP server.

Two things are never overridable by a per-key policy file, regardless of what it contains:

- **Bubblewrap stays the enforced backend** (`Enabled: true`, `Backend: bwrap`) — a key cannot disable sandboxing entirely, even by omitting or zeroing the `sandbox:` section.
- **The real workspace directory is always bind-mounted read-write** into the sandbox (on the file's `Workdir`, or `/work` if unset) — a key's policy can add other binds or allow network, but can never cause two tenants to share a directory or lose their isolation.

The file is validated at boot (fail-fast): a missing path, malformed YAML, or invalid template syntax in any `LEASH_APIKEY_<NAME>_POLICY` prevents the server from starting.

### Giving an MCP server access to the session's own workspace

`stdio` MCP servers declared in `mcp_servers` are spawned directly on the **host**, not inside the tenant's bubblewrap sandbox. So a hardcoded in-sandbox path like `/work` is meaningless to them — that path only exists inside the mount namespace of a running `execute_shell` call. To give such a server access to the _real_ directory backing the current session's workspace (the same directory bind-mounted into the sandbox), the policy file is rendered as a [Go template](https://pkg.go.dev/text/template) before being loaded, with two values available:

| Placeholder         | Value                                                            |
| ------------------- | ---------------------------------------------------------------- |
| `{{.WorkspaceDir}}` | Real host path of this session's workspace directory.            |
| `{{.WorkspaceID}}`  | HMAC hash identifying this workspace (the directory's basename). |

```dotenv
LEASH_APIKEY_RESEARCH=...
LEASH_APIKEY_RESEARCH_POLICY=policies/research-with-mcp.yaml
```

```yaml
# policies/research-with-mcp.yaml
allowed_binaries: ["curl", "jq"]
mcp_servers:
  - name: filesystem
    transport: stdio
    # {{.WorkspaceDir}} resolves, at the time *this* workspace is created,
    # to the real host directory bind-mounted into the sandbox — so the
    # filesystem server and the sandboxed shell see the same files.
    command:
      [
        "npx",
        "-y",
        "@modelcontextprotocol/server-filesystem",
        "{{.WorkspaceDir}}",
      ]
sandbox:
  unshare:
    network: false # allow outbound network for this tenant's MCP server
    pid: true
    ipc: true
    uts: true
    user: true
  die_with_parent: true
```

Each workspace gets its own engine (and thus its own MCP server subprocess), so the template is re-rendered with the correct `WorkspaceDir` every time a new tenant workspace is created — never shared across tenants. The rendered file is written to a temporary path outside the workspace directory (so it's never visible from inside the tenant's own sandboxed shell) and removed when the workspace is closed.

## Environment variables

| Variable                           | Required  | Default              | Description                                                                                                |
| ---------------------------------- | --------- | -------------------- | ---------------------------------------------------------------------------------------------------------- |
| `LEASH_HMAC_SECRET`                | yes       | —                    | Secret key used to derive workspace directory names from discriminants.                                    |
| `LEASH_APIKEY_<NAME>`              | yes (×1+) | —                    | Raw value of an API key named `<NAME>`. At least one is required.                                          |
| `LEASH_APIKEY_<NAME>_WORKSPACE_ID` | no        | —                    | Fixes the workspace for this key, overriding any header/URL discriminant.                                  |
| `LEASH_APIKEY_<NAME>_ENV`          | no        | —                    | Comma-separated `K=V` pairs injected as static env vars for this key's workspace.                          |
| `LEASH_APIKEY_<NAME>_POLICY`       | no        | —                    | Path to a per-key policy YAML file (binaries, builtins, MCP servers, sandbox). See "Per-key policy" below. |
| `LEASH_WORKSPACE_ROOT`             | no        | `./leash-workspaces` | Root directory under which workspace directories are created.                                              |
| `LEASH_TTL`                        | no        | `30m`                | Inactivity TTL before a workspace is reaped (Go duration syntax, e.g. `1h`).                               |
| `LEASH_DISC_HEADER`                | no        | `X-Workspace`        | Name of the HTTP header carrying the discriminant.                                                         |
| `LEASH_DISC_URL_PARAM`             | no        | `workspace`          | Name of the URL path variable / query parameter carrying the discriminant.                                 |
| `LEASH_MCP_LISTEN_ADDR`            | no        | `:8443`              | TCP listen address of the HTTP server.                                                                     |
| `LEASH_SANDBOX_BACKEND`            | no        | `bwrap`              | Sandbox backend enforced for every workspace: `bwrap` or `chroot`. See "Sandbox backend" below.            |
| `LEASH_MAX_FILE_BYTES`             | no        | `33554432` (32 MiB)  | Maximum size of a single file accepted by `PUT /files/…`.                                                  |
| `LEASH_WORKSPACE_QUOTA_BYTES`      | no        | `268435456` (256 MiB)| Maximum cumulated size of a workspace's files. `0` disables the quota.                                      |

If both the header and the URL variable are present on a request, **the header takes priority**.

## Sandbox backend

The sandbox backend is chosen by the **server operator**, never by an API key:

- `LEASH_SANDBOX_BACKEND=bwrap` (default) — bubblewrap. Network, PID, IPC, UTS and user namespaces are unshared, and the workspace directory is the only read-write mount, on `/work`.
- `LEASH_SANDBOX_BACKEND=chroot` — operational fallback for hosts where bubblewrap cannot run. The chroot backend knows nothing about bind mounts or namespaces: it cannot cut the network, and the workspace directory becomes the process working directory directly (no `/work`). When it is selected, **the effective security boundary is whatever isolates the server process itself** — typically the container it runs in. Only choose it knowingly.

A per-key policy file can still shape `allowed_binaries`, builtins, MCP servers and the rest of the `sandbox:` section, but it can neither disable sandboxing nor pick a different backend.

### Running bubblewrap inside a container

Bubblewrap needs three things a container runtime denies by default. Under Docker, the working combination is:

```bash
docker run \
  --security-opt seccomp=unconfined \
  --security-opt apparmor=unconfined \
  --security-opt systempaths=unconfined \
  …
```

- `seccomp=unconfined` — the default seccomp profile blocks the `clone` flags needed to create an unprivileged user namespace.
- `apparmor=unconfined` — the `docker-default` AppArmor profile refuses `mount --make-rslave /`, which bwrap does before pivoting.
- `systempaths=unconfined` — Docker masks parts of `/proc`; while `/proc` is not fully visible, the kernel refuses to let a new procfs be mounted inside the namespace.

Root is *not* required: the combination above works for an unprivileged container user. If these options cannot be granted, fall back to `LEASH_SANDBOX_BACKEND=chroot` (which does require root) and accept the reduced guarantees described above.

## Files endpoints

Binary payloads have no place in MCP tool results, which are textual and size-bounded. The server therefore exposes a small file API next to the MCP route, sharing its authentication, its discriminant resolution and its workspace lifecycle — a file request refreshes a workspace's TTL exactly like a tool call does.

| Method   | Path            | Description                                                                       |
| -------- | --------------- | --------------------------------------------------------------------------------- |
| `PUT`    | `/files/{path}` | Write the request body to `{path}` in the caller's workspace. `201` + JSON result. |
| `GET`    | `/files/{path}` | Return the file, with a `Content-Type` guessed from its extension.                 |
| `DELETE` | `/files/{path}` | Delete the file (or directory) at `{path}`. `204`.                                 |
| `GET`    | `/files/`       | List every regular file: `{"files":[{"path":…,"size":…}],"total_bytes":…}`.        |

Rules:

- Same `Authorization: Bearer` header and same discriminant (`X-Workspace` by default) as the MCP route. Unlike the MCP route, a missing discriminant is a `400`: no ephemeral workspace is created, since a file dropped in a workspace nobody can address again would only hide a client misconfiguration.
- Paths are confined with [`os.Root`](https://pkg.go.dev/os#Root), not with a `Clean`-then-prefix check. A prefix check only inspects the string, so it still lets through a path whose intermediate component is a symlink out of the workspace — and the tenant's own shell can create such a symlink. `os.Root` resolves each component in the kernel and refuses to follow a link leaving the root.
- A file larger than `LEASH_MAX_FILE_BYTES` is rejected with `413`; a write that would push the workspace past `LEASH_WORKSPACE_QUOTA_BYTES` is rejected with `507`. Overwriting an existing file does not count its previous size twice.

```bash
curl -X PUT --data-binary @clip.mp4 \
  -H "Authorization: Bearer $API_KEY" \
  -H "X-Workspace: tenant-42" \
  http://localhost:8443/files/clip.mp4
```

## Example request

```bash
curl -X POST http://localhost:8443/ \
  -H "Authorization: Bearer $API_KEY" \
  -H "X-Workspace: tenant-42" \
  -H "Content-Type: application/json" \
  -H "Accept: application/json, text/event-stream" \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"execute_shell","arguments":{"script":"echo hello"}}}'
```

## Exposed tools

| Tool            | Description                                                                                                              |
| --------------- | ------------------------------------------------------------------------------------------------------------------------ |
| `execute_shell` | Run an arbitrary sandboxed script in the caller's workspace; returns stdout, stderr, exit code, and any BLOCKED messages |

## Logs

Two independent log streams, both on `os.Stderr`:

- **Audit (JSON, one line per command)**: every command executed in every workspace — builtin or binary, allowed or blocked — is logged with `workspace_id` and `api_key` attributes, e.g.:
  ```json
  {
    "time": "...",
    "level": "INFO",
    "msg": "command executed",
    "workspace_id": "a1b2...",
    "api_key": "tenant-a",
    "command": "ls",
    "args": ["/tmp"],
    "duration": 11630091,
    "exit_code": 0,
    "blocked": false,
    "is_builtin": false,
    "sandbox": "bwrap"
  }
  ```
  Blocked commands are logged at `WARN` with a `reason` field instead. This is always on for every workspace, regardless of `LEASH_LOG_LEVEL`.
- **Debug (text, request lifecycle)**: workspace creation/reuse/reaping (`Manager`), each `execute_shell` call's script/exit code/duration (`Workspace.Exec`), and the actual MCP tool response/error sent back to the agent (`handleExecuteShell`: full formatted text on success at `DEBUG`, `WARN` for tool-level errors like invalid/missing parameters, `ERROR` if the engine itself fails) — all tagged with the same `workspace_id`/`api_key`. Controlled by `LEASH_LOG_LEVEL` (see below) — set it to `debug` to see the response bodies; tool-level `WARN`/`ERROR` entries show at the default level.

| Variable          | Required | Default | Description                                                                                     |
| ----------------- | -------- | ------- | ----------------------------------------------------------------------------------------------- |
| `LEASH_LOG_LEVEL` | no       | `info`  | `debug`, `info`, `warn`, or `error`. Only affects the debug stream above, not the audit stream. |

## Graceful shutdown

`server` listens for `SIGINT`/`SIGTERM`, stops accepting new connections, drains in-flight requests, and closes every active workspace (cleanup + directory removal) before exiting.
