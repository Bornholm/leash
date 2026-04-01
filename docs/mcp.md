# MCP server

LeaSH can run as an [MCP](https://modelcontextprotocol.io) server, exposing the execution engine as a set of tools for LLMs.

## Transports

### stdio (recommended for local use)

```bash
./leash --policy policies/default.yaml mcp stdio
```

### HTTP

```bash
./leash --policy policies/default.yaml mcp http --addr :8080
```

## Exposed tools

| Tool | Description |
|------|-------------|
| `execute_shell` | Run an arbitrary sandboxed script; returns stdout, stderr, exit code, and any BLOCKED messages |
| `list_commands` | Return the manifest of available commands as Markdown (useful for LLM system prompts) |

## Client configuration

### Claude Desktop

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

### Docker

```bash
docker run --rm -i ghcr.io/bornholm/leash:latest \
  --policy /policies/default.yaml mcp stdio
```

## Bridging external MCP servers

LeaSH can forward external MCP server tools into the shell environment, making them available as skills inside scripts.

```yaml
mcp_servers:
  - name: filesystem
    transport: stdio
    command: ["npx", "-y", "@modelcontextprotocol/server-filesystem", "/tmp"]
  - name: my-api
    transport: stdio
    command: ["/usr/local/bin/my-mcp-server"]
```

Each bridged tool becomes callable as a skill inside scripts.
