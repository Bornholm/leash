# Policy files

Policies are YAML files that define what the engine is allowed to do. Pass one with `--policy`.

Four presets are provided in `policies/`:

| File               | Description                                       |
| ------------------ | ------------------------------------------------- |
| `default.yaml`     | Balanced — common read-only binaries, 30s timeout |
| `restrictive.yaml` | Minimal binary set, tight limits                  |
| `permissive.yaml`  | Wider binary set, relaxed limits                  |
| `sandboxed.yaml`   | Bubblewrap filesystem isolation example           |

## Reference

```yaml
execution:
  max_duration: 30s # Maximum script runtime (go duration string)
  max_output_bytes: 1048576 # Maximum combined stdout+stderr (bytes)
  max_commands_per_script: 50 # Maximum commands per script
  max_subshells: 3 # Maximum subshell nesting depth

rate_limits:
  global: 100/minute # Global limit across all commands
  per_skill:
    my-skill: 10/minute # Per-skill override

allowed_binaries: # Whitelist — everything else returns exit 127
  - grep
  - sed
  - awk
  - sort
  - head
  - tail
  - cat
  - echo

blocked_patterns: # Checked on expanded args after variable interpolation (post-expansion)
  - "rm -rf"
  - "mkfs"
  - "dd if="
  - "token=secret"

environment:
  inherit: false # Never inherit the host environment (recommended)
  static:
    HOME: /tmp/leash
    PATH: /usr/bin:/bin

skills:
  enabled: [] # Empty = all registered skills allowed; list names to restrict
  require_confirmation: [] # Skills that require CONFIRM_<NAME>=yes in the environment

# Bridge external MCP servers into the shell environment
# mcp_servers:
#   - name: filesystem
#     transport: stdio
#     command: ["npx", "-y", "@modelcontextprotocol/server-filesystem", "/tmp"]
#     env:                         # Environment variables FOR the MCP server process
#       SOME_VAR: value           # (not injected into shell commands)

sandbox:
  enabled: false            # Set to true to activate filesystem isolation
  backend: bwrap            # none | bwrap | chroot
  readonly_binds:           # Paths exposed read-only inside the sandbox
    - /usr
  readwrite_binds:          # Paths exposed read-write inside the sandbox
    - source: /tmp/work
      target: /work
  tmpfs:                    # Paths mounted as ephemeral tmpfs (or shared — see below)
    - /tmp
  symlinks:                 # Symlinks to create inside the namespace (merged-usr systems)
    - source: usr/bin
      target: /bin
  workdir: /work            # Working directory inside the sandbox
  unshare:
    network: true           # Isolate network namespace
    pid: true               # Isolate PID namespace
    ipc: true               # Isolate IPC namespace
    uts: true               # Isolate UTS (hostname) namespace
  die_with_parent: true     # Kill sandbox if the parent process dies
  persistent_tmp: false     # Share /tmp across all commands within a script execution
```

## Environment variable interpolation

Any value in a policy file can reference environment variables using shell-style syntax. Expansion happens before YAML parsing, so it works in any field.

| Syntax              | Behavior                                           |
| ------------------- | -------------------------------------------------- |
| `$VAR`              | Replaced by the value of `VAR`, empty if unset     |
| `${VAR}`            | Same as above                                      |
| `${VAR:-default}`   | Uses `default` if `VAR` is unset **or empty**      |
| `${VAR-default}`    | Uses `default` only if `VAR` is **unset**          |

Example — parameterising paths via the environment:

```yaml
environment:
  static:
    HOME: ${LEASH_HOME:-/tmp/leash}
    DATA_DIR: ${MY_DATA_DIR}

mcp_servers:
  - name: my-server
    transport: stdio
    command: ["${SERVER_BIN:-/usr/local/bin/my-mcp-server}", "--config", "$CONFIG_PATH"]
```

## MCP servers

External MCP servers are started as subprocesses and connected via stdio or HTTP.

| Attribute   | Description                                  |
| ----------- | -------------------------------------------- |
| `name`      | Server identifier (used in logs)             |
| `transport` | `stdio`, `http`, or `sse`                    |
| `command`   | argv array (for stdio)                       |
| `env`       | Environment variables for the server process |
| `url`       | Endpoint URL (for http/sse)                  |
| `headers`   | HTTP headers (for http/sse)                  |

### `environment` vs `env`

- **`environment`** (section at root level): variables injected into shell commands executed by the engine
- **`env`** (inside `mcp_servers`): variables passed to the MCP server subprocess itself

Example:

```yaml
environment:
  static:
    MY_VAR: "visible to shell commands"

mcp_servers:
  - name: my-mcp
    transport: stdio
    command: ["npx", "-y", "@my/mcp-server"]
    env:
      MY_VAR: "foobar" # only visible to the MCP server
```

## Rate limit format

Values follow the pattern `N/unit` where unit is `second` or `minute`.

```yaml
rate_limits:
  global: 60/minute
  per_skill:
    fetch: 10/minute
    heavy-task: 2/minute
```
