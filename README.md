<p align="center">
  <img src="https://raw.githubusercontent.com/Bornholm/leash/refs/heads/master/misc/assets/logo.svg" width="256px" alt="Logo"/>
</p>

# LeaSH — **L**LM **E**xecution **A**udited **SH**ell

A policy-enforced shell execution engine for LLMs and agents.

## Getting started

### Install

```bash
git clone https://github.com/bornholm/leash.git
cd leash
make build          # produces ./leash
```

Or download a pre-built binary from the [Releases page](https://github.com/bornholm/leash/releases).

### Run

```bash
# Interactive REPL
./leash --policy policies/default.yaml repl

# One-shot execution
./leash --policy policies/default.yaml exec --exec 'echo hello | tr a-z A-Z'

# MCP server (stdio, for Claude Desktop and other MCP clients)
./leash --policy policies/default.yaml mcp stdio
```

### Use as an MCP tool

Add to your MCP client configuration (e.g. `claude_desktop_config.json`):

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

## Features

- **Binary allowlist** — only explicitly listed system commands can run
- **AST validation** — command count, subshell depth, and background job limits enforced before execution
- **Pattern blocking** — substring matches reject dangerous commands before parsing
- **Environment isolation** — host environment never inherited; only declared variables are visible
- **Rate limiting** — global and per-skill call rate limits
- **Timeout** — configurable maximum execution duration per script
- **Audit trail** — every command (blocked or executed) logged as structured JSON
- **Filesystem sandbox** — bubblewrap (bwrap) or chroot isolation; only bind-mounted paths are accessible
- **MCP transport** — expose as an MCP tool server for Claude Desktop and other agents
- **Extensible builtins** — register Go functions, Tengo scripts, or shell scripts as shell commands

### Filesystem sandbox example

```bash
# Install bubblewrap
apt install bubblewrap   # or: pacman -S bubblewrap

# Create the sandbox work directory
mkdir -p /tmp/leash-sandbox

# Run a command: ls /work is isolated to /tmp/leash-sandbox
echo 'ls /work' | ./leash --policy policies/sandboxed.yaml exec

# /etc is not bind-mounted → cat /etc/shadow fails
echo 'cat /etc/shadow' | ./leash --policy policies/sandboxed.yaml exec
```

## Documentation

- [CLI reference](docs/cli.md) — all commands and flags (includes sandbox YAML reference)
- [Policy files](docs/policies.md) — control what the engine is allowed to do
- [Builtins — Go](docs/builtins.md) — register custom Go functions as shell commands
- [Builtins — Tengo](docs/builtins-tengo.md) — write builtins as Tengo scripts (no compilation)
- [Builtins — Shell](docs/builtins-shell.md) — write builtins as POSIX shell scripts
- [MCP server](docs/mcp.md) — expose LeaSH as an MCP tool set
- [Go library](docs/library.md) — embed LeaSH in your own application
- [Security model](docs/security.md) — how isolation and enforcement work

## License

GPL-3.0
