# CLI reference

## Global flags

These flags apply to all subcommands.

| Flag | Env var | Default | Description |
|------|---------|---------|-------------|
| `--policy`, `-p` | `LEASH_POLICY` | `policies/default.yaml` | Path to the YAML policy file |
| `--audit-log`, `-A` | `LEASH_AUDIT_LOG` | _(stderr)_ | File to write JSON audit logs to |
| `--tengo-skills` | — | — | Directory of Tengo skill scripts (`*.tengo`); repeatable |
| `--shell-skills` | — | — | Directory of POSIX shell skill scripts (`*.sh`); repeatable |

## Commands

### `leash repl`

Start an interactive REPL. Type shell commands at the prompt; they are executed through the engine and policy.

```bash
./leash --policy policies/default.yaml repl
./leash --policy policies/default.yaml --tengo-skills ./skills/tengo repl
```

### `leash exec`

Execute a script and exit. The script can come from three sources (checked in order):

1. `--exec` flag
2. A file path passed as argument
3. stdin

```bash
# Inline script
./leash --policy policies/default.yaml exec --exec 'echo hello | tr a-z A-Z'

# Script file
./leash --policy policies/default.yaml exec script.sh

# stdin
echo 'date' | ./leash --policy policies/default.yaml exec
```

The process exits with the script's exit code.

### `leash mcp stdio`

Start an MCP server on stdin/stdout. Use this with Claude Desktop and other MCP clients that spawn a subprocess.

```bash
./leash --policy policies/default.yaml mcp stdio
```

### `leash mcp http`

Start an MCP server over HTTP.

| Flag | Env var | Default | Description |
|------|---------|---------|-------------|
| `--addr`, `-a` | `LEASH_MCP_ADDR` | `:8080` | Listen address |

```bash
./leash --policy policies/default.yaml mcp http --addr :9000
```

## Loading skills

### Tengo skills

Point `--tengo-skills` at a directory containing `*.tengo` files. The flag can be repeated to load from multiple directories.

```bash
./leash --policy policies/default.yaml \
        --tengo-skills ./skills/text \
        --tengo-skills ./skills/data \
        repl
```

See [Skills — Tengo](skills-tengo.md) for how to write Tengo skill files.

### Shell skills

Point `--shell-skills` at a directory containing `*.sh` files. The flag can be repeated to load from multiple directories.

```bash
./leash --policy policies/default.yaml \
        --shell-skills ./skills/text \
        exec --exec 'echo "hello world" | uppercase'
```

See [Skills — Shell](skills-shell.md) for how to write shell skill files.

### Combining both

```bash
./leash --policy policies/default.yaml \
        --tengo-skills ./skills/tengo \
        --shell-skills ./skills/shell \
        mcp stdio
```

Skills from all directories are merged into the same registry. Registration fails if two skills share the same name.

## Sandbox YAML reference

The `sandbox` block can appear in any policy file. All fields are optional except `enabled` and `backend`.

```yaml
sandbox:
  enabled: true          # false = backend none (default)
  backend: bwrap         # none | bwrap | chroot

  # bwrap — read-only bind mounts (host path → same path in namespace)
  readonly_binds:
    - /usr
    - /etc/ssl           # certificates for TLS

  # bwrap — read-write bind mounts (source → target inside namespace)
  readwrite_binds:
    - source: /tmp/leash-work
      target: /work

  # bwrap — tmpfs mounts (in-memory, empty at start)
  tmpfs:
    - /tmp

  # bwrap — symlinks to create inside the namespace
  # Required on merged-usr systems where /bin → usr/bin etc.
  symlinks:
    - source: usr/bin    # symlink value (relative to sandbox root)
      target: /bin       # path inside the namespace

  # bwrap — working directory inside the namespace
  workdir: /work

  # bwrap — Linux namespace isolation
  unshare:
    network: true        # isolate network (no outbound connections)
    pid: true            # isolate PID namespace
    ipc: true            # isolate IPC namespace
    uts: true            # isolate hostname/domainname
    user: false          # isolate user namespace (requires user namespaces)

  # bwrap — kill sandboxed process if leash exits
  die_with_parent: true

  # chroot — path to the rootfs (required for chroot backend)
  rootfs: /var/lib/leash/rootfs

  # chroot — run as this UID/GID inside the rootfs
  uid: 1000
  gid: 1000
```

### Install bubblewrap

```bash
# Debian / Ubuntu
apt install bubblewrap

# Fedora / RHEL
dnf install bubblewrap

# Arch / Manjaro
pacman -S bubblewrap
```

## Examples

```bash
# REPL with custom Tengo skills
./leash --policy policies/default.yaml \
        --tengo-skills examples/tengo/skills \
        repl

# One-shot with shell skills, reading from stdin
echo 'echo "hello world" | uppercase' | \
  ./leash --policy policies/default.yaml \
          --shell-skills examples/shell/skills \
          exec

# MCP server exposing Tengo + shell skills to Claude Desktop
./leash --policy policies/default.yaml \
        --tengo-skills /opt/leash/skills/tengo \
        --shell-skills /opt/leash/skills/shell \
        mcp stdio
```
