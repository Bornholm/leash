# LeaSH — **L**LM **E**xecution **A**udited **SH**ell

LeaSH is a policy-enforced shell execution engine designed to let LLMs and agents run shell commands safely. It exposes a **CLI**, an **MCP server**, and a **Go library**.

Every script is validated against a configurable policy before execution: allowed binaries, blocked patterns, resource limits, rate limiting, and audit logging are all enforced at the engine level.

## Features

- **Policy-enforced execution** — allowlist of binaries, blocked patterns, subshell depth, command count
- **Custom skills** — register virtual commands (Go functions) alongside real binaries
- **MCP server** — expose the engine as an MCP tool set (stdio or HTTP) for use with Claude and other LLMs
- **Go library** — embed LeaSH in your own application via the `pkg/leash` OptionFunc API
- **Audit trail** — every command execution is logged (JSON via `log/slog`)
- **Rate limiting** — global and per-skill request limits
- **External MCP servers** — bridge external MCP tools into the shell environment

## Installation

### Pre-built binaries

Download the latest release from the [Releases page](https://github.com/bornholm/leash/releases).

### From source

```bash
git clone https://github.com/bornholm/leash.git
cd leash
make build          # produces ./leash
```

### Docker

```bash
docker pull ghcr.io/bornholm/leash:latest
docker run --rm ghcr.io/bornholm/leash:latest --help
```

## Quick Start

### Interactive REPL

```bash
./leash --policy policies/default.yaml repl
```

### One-shot execution

```bash
echo 'echo hello | tr a-z A-Z' | ./leash --policy policies/default.yaml exec
./leash --policy policies/default.yaml exec --exec 'date'
```

### MCP server (stdio)

```bash
./leash --policy policies/default.yaml mcp stdio
```

### MCP server (HTTP)

```bash
./leash --policy policies/default.yaml mcp http --addr :8080
```

## Policy Files

Policies are YAML files that control what the engine is allowed to do.

```yaml
# policies/default.yaml
execution:
  max_duration: 30s
  max_output_bytes: 1048576 # 1 MiB
  max_commands_per_script: 50
  max_subshells: 3

rate_limits:
  global: 100/minute
  per_skill:
    my-skill: 10/minute

allowed_binaries:
  - grep
  - sed
  - awk
  - sort
  - head
  - tail
  - cat
  - echo

blocked_patterns:
  - "rm -rf"
  - "mkfs"
  - "dd if="

environment:
  inherit: false # never inherit host environment
  static:
    HOME: /tmp/leash
    PATH: /usr/bin:/bin

skills:
  enabled: [] # empty = all registered skills allowed
  require_confirmation: []
# Optional: bridge external MCP servers into the shell environment
# mcp_servers:
#   - name: filesystem
#     transport: stdio
#     command: ["npx", "-y", "@modelcontextprotocol/server-filesystem", "/tmp"]
```

Three presets are provided: `policies/default.yaml`, `policies/restrictive.yaml`, `policies/permissive.yaml`.

## Go Library

LeaSH can be embedded as a Go library using the `pkg/leash` package.

```go
import (
    "github.com/bornholm/leash/pkg/leash"
    "github.com/bornholm/leash/pkg/skill"
)
```

### Creating an Engine

```go
eng, cleanup, err := leash.New(ctx,
    leash.WithMaxDuration(30 * time.Second),
    leash.WithAllowedBinaries("grep", "awk", "sort", "head"),
    leash.WithEnvVar("HOME", "/tmp/sandbox"),
    leash.WithAuditWriter(os.Stderr),
)
if err != nil {
    log.Fatal(err)
}
defer cleanup()
```

### Loading a policy file

Options applied after `WithPolicyFile` override individual fields from the file.

```go
eng, cleanup, err := leash.New(ctx,
    leash.WithPolicyFile("policies/restrictive.yaml"),
    leash.WithEnvVar("MY_VAR", "value"),  // overrides the file
)
```

### Registering custom skills

Skills are Go functions exposed as shell commands inside scripts.

```go
wordCount := skill.New("word-count").
    Description("Count words from stdin").
    Category("text").
    Handle(func(ctx context.Context, c *skill.Call) error {
        data, _ := io.ReadAll(c.Stdin)
        fmt.Fprintf(c.Stdout, "%d\n", len(strings.Fields(string(data))))
        return nil
    })

eng, cleanup, err := leash.New(ctx,
    leash.WithSkill(wordCount),
)
```

Skills support positional arguments, named flags, and rate limiting:

```go
skill.New("fetch").
    Description("Fetch a URL and return its body").
    Arg("url", "Target URL", true).
    Flag("timeout", "t", "10s", "Request timeout").
    RateLimit(30).   // 30 calls/minute
    Handle(myHandler)
```

Inside a script, skills are called like regular commands:

```bash
echo "hello world foo" | word-count
fetch https://example.com --timeout=5s | grep title
```

### Executing scripts

```go
// Capture stdout/stderr
result, err := eng.Exec(ctx, `echo hello | tr a-z A-Z`)

fmt.Printf("stdout: %s\n", result.Stdout)
fmt.Printf("exit:   %d\n", result.ExitCode)
fmt.Printf("took:   %s\n", result.Duration)

// Stream to your own writers
err = eng.ExecWithStreams(ctx, script, os.Stdin, os.Stdout, os.Stderr)
```

### Audit trail

```go
result, _ := eng.Exec(ctx, `grep foo /var/log/app.log | head -20`)

for _, cmd := range result.Audit.Commands {
    if cmd.Blocked {
        fmt.Printf("BLOCKED %s: %s\n", cmd.Command, cmd.Reason)
    } else {
        fmt.Printf("OK      %s (exit %d, %s)\n", cmd.Command, cmd.ExitCode, cmd.Duration)
    }
}
```

### Available options

| Option                                    | Description                                     |
| ----------------------------------------- | ----------------------------------------------- |
| `WithPolicyFile(path)`                    | Load base config from a YAML policy file        |
| `WithMaxDuration(d)`                      | Maximum script execution time                   |
| `WithMaxOutputBytes(n)`                   | Maximum combined stdout+stderr size             |
| `WithMaxCommandsPerScript(n)`             | Maximum number of commands per script           |
| `WithMaxSubshells(n)`                     | Maximum subshell depth                          |
| `WithGlobalRateLimit(count, window)`      | Global rate limit across all commands           |
| `WithSkillRateLimit(name, count, window)` | Per-skill rate limit                            |
| `WithAllowedBinaries(names...)`           | Declare the complete binary allowlist           |
| `WithBlockedPatterns(patterns...)`        | Declare the complete blocked pattern list       |
| `WithStaticEnv(map)`                      | Replace the entire static environment           |
| `WithEnvVar(key, value)`                  | Add or override a single environment variable   |
| `WithEnabledSkills(names...)`             | Whitelist specific skills (empty = all allowed) |
| `WithRequireConfirmation(names...)`       | Skills requiring `CONFIRM_<NAME>=yes`           |
| `WithSkill(s)`                            | Register a single skill                         |
| `WithSkills(s...)`                        | Register multiple skills                        |
| `WithMCPServer(cfg)`                      | Connect an external MCP server                  |
| `WithAuditWriter(w)`                      | Destination for JSON audit logs                 |

## Example

A runnable example is available in [`examples/basic/`](examples/basic/main.go):

```bash
go run ./examples/basic/
```

It demonstrates custom skills, pipelines, audit trail inspection, and blocked command handling.

## MCP Integration

When running as an MCP server, LeaSH exposes:

- **`execute_shell`** — run an arbitrary sandboxed script
- **`list_commands`** — return the manifest of available commands (for LLM system prompts)
- **`skill_<name>`** — one tool per registered skill, auto-generated from metadata

Configure it in your MCP client (e.g. Claude Desktop):

```json
{
  "mcpServers": {
    "leash": {
      "command": "/path/to/leash",
      "args": ["--policy", "/path/to/policy.yaml", "mcp", "stdio"]
    }
  }
}
```

## Security model

- The host environment is **never inherited** (`environment.inherit: false` by default)
- Background jobs (`&`) are rejected at the AST level
- Blocked patterns are checked **before parsing** (fast path for obvious attacks)
- All command executions are recorded in the audit trail regardless of outcome
- Binaries not in `allowed_binaries` return exit code 127 (not an OS error)

## Development

```bash
make build          # compile → ./leash
make test           # go test ./...
make lint           # go vet + golangci-lint
make run-repl       # REPL with policies/default.yaml
make run-mcp        # MCP stdio mode
```

## License

GPL-3.0
