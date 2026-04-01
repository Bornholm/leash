# Policy files

Policies are YAML files that define what the engine is allowed to do. Pass one with `--policy`.

Three presets are provided in `policies/`:

| File               | Description                                       |
| ------------------ | ------------------------------------------------- |
| `default.yaml`     | Balanced — common read-only binaries, 30s timeout |
| `restrictive.yaml` | Minimal binary set, tight limits                  |
| `permissive.yaml`  | Wider binary set, relaxed limits                  |

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

blocked_patterns: # Substring matches checked before parsing
  - "rm -rf"
  - "mkfs"
  - "dd if="

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
