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

## Documentation

- [CLI reference](docs/cli.md) — all commands and flags
- [Policy files](docs/policies.md) — control what the engine is allowed to do
- [Skills — Go](docs/skills.md) — register custom Go functions as shell commands
- [Skills — Tengo](docs/skills-tengo.md) — write skills as Tengo scripts (no compilation)
- [Skills — Shell](docs/skills-shell.md) — write skills as POSIX shell scripts
- [MCP server](docs/mcp.md) — expose LeaSH as an MCP tool set
- [Go library](docs/library.md) — embed LeaSH in your own application
- [Security model](docs/security.md) — how isolation and enforcement work

## License

GPL-3.0
