package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/bornholm/leash/internal/engine"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// MCPServer expose le moteur LeaSH via le protocole MCP (SDK officiel).
type MCPServer struct {
	eng    engine.Engine
	server *mcp.Server
}

// New crée un MCPServer et enregistre tous les tools.
func New(eng engine.Engine) *MCPServer {
	s := mcp.NewServer(
		&mcp.Implementation{Name: "LeaSH", Version: "1.0.0"},
		&mcp.ServerOptions{
			Instructions: buildInstructions(eng),
		},
	)
	ms := &MCPServer{eng: eng, server: s}
	ms.registerTools()
	return ms
}

// buildInstructions generates server instructions for the LLM agent based on the
// registered commands and available binaries.
func buildInstructions(eng engine.Engine) string {
	var sb strings.Builder

	sb.WriteString(`LeaSH is a policy-enforced shell sandbox. All commands run inside the sandbox and must comply with the active policy.

## Available tools

### execute_shell
Execute a shell script inside the sandbox. Use this for EVERYTHING:
- System binaries (ls, grep, curl, etc.)
- Shell builtins (echo, cd, export, etc.)
- Pipes, loops, conditionals, and any shell syntax
- Domain specific tools configured in the shell environment

Returns: STDOUT, STDERR, EXIT CODE, and BLOCKED section if any command was denied.

### list_commands
List all available commands. Call this first to discover what commands are registered and how to use them.

`)

	// Available commands = registered commands + allowed binaries
	commands := eng.Registry().ListNames()
	binaries := eng.Policy().AllowedBinaries()

	if len(binaries) == 0 && len(commands) == 0 {
		sb.WriteString("## Available commands\nNo commands are available.\n")
	} else {
		sb.WriteString("## Available commands\n")
		for _, c := range commands {
			sb.WriteString("- " + c + "\n")
		}
		for _, b := range binaries {
			sb.WriteString("- " + b + "\n")
		}
		sb.WriteString("\nAny other command will be blocked (exit code 127).\n")
	}

	sb.WriteString(`
## Decision guide

1. Need to run a command or script? → use execute_shell
2. Want to discover available commands? → call list_commands first
3. Command returns exit code 127? → blocked by policy, do not retry
`)

	return sb.String()
}

// ServeStdio démarre le serveur MCP sur stdin/stdout.
func (ms *MCPServer) ServeStdio() error {
	return ms.server.Run(context.Background(), &mcp.StdioTransport{})
}

// ServeHTTP démarre le serveur MCP en HTTP Streamable sur l'adresse donnée.
func (ms *MCPServer) ServeHTTP(addr string) error {
	handler := mcp.NewStreamableHTTPHandler(
		func(_ *http.Request) *mcp.Server { return ms.server },
		nil,
	)
	return http.ListenAndServe(addr, handler) //nolint:gosec
}

func (ms *MCPServer) registerTools() {
	// Tool execute_shell
	ms.server.AddTool(
		&mcp.Tool{
			Name:        "execute_shell",
			Description: "Execute a shell script inside the LeaSH sandbox. Returns stdout, stderr, exit code, and any blocked commands.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"script": map[string]any{
						"type":        "string",
						"description": "Shell script to execute",
					},
				},
				"required": []string{"script"},
			},
		},
		ms.handleExecuteShell,
	)

	// Tool list_commands
	ms.server.AddTool(
		&mcp.Tool{
			Name:        "list_commands",
			Description: "List all available skills with their documentation.",
			InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
		},
		ms.handleListCommands,
	)
}

func (ms *MCPServer) handleExecuteShell(_ context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	var args map[string]any
	if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
		return errorResult("invalid parameters: " + err.Error()), nil
	}
	script, ok := args["script"].(string)
	if !ok || script == "" {
		return errorResult("parameter 'script' is required"), nil
	}

	result, err := ms.eng.Exec(context.Background(), script)
	if err != nil {
		return errorResult(fmt.Sprintf("execution error: %v", err)), nil
	}

	var sb strings.Builder
	if len(result.Stdout) > 0 {
		sb.WriteString("## STDOUT\n")
		sb.Write(result.Stdout)
		sb.WriteString("\n")
	}
	if len(result.Stderr) > 0 {
		sb.WriteString("## STDERR\n")
		sb.Write(result.Stderr)
		sb.WriteString("\n")
	}
	fmt.Fprintf(&sb, "## EXIT CODE\n%d\n", result.ExitCode)

	if result.Audit != nil {
		var blocked []string
		for _, cmd := range result.Audit.Commands {
			if cmd.Blocked {
				blocked = append(blocked, fmt.Sprintf("- `%s`: %s", cmd.Command, cmd.Reason))
			}
		}
		if len(blocked) > 0 {
			sb.WriteString("## BLOCKED\n")
			sb.WriteString(strings.Join(blocked, "\n"))
			sb.WriteString("\n\n→ Use list_commands to see available commands\n")
		}
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: sb.String()}},
		IsError: result.ExitCode != 0,
	}, nil
}

func (ms *MCPServer) handleListCommands(_ context.Context, _ *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	var sb strings.Builder

	commands := ms.eng.Registry().ListNames()
	binaries := ms.eng.Policy().AllowedBinaries()

	if len(commands) > 0 {
		sb.WriteString("# Available Commands\n\n")
		sb.WriteString(ms.eng.Registry().GenerateManifest())
	}

	if len(binaries) > 0 {
		sb.WriteString("# System Binaries\n\n")
		sb.WriteString("The following system binaries can be called directly:\n\n")
		for _, b := range binaries {
			sb.WriteString("- " + b + "\n")
		}
		sb.WriteString("\n")
	}

	if len(commands) == 0 && len(binaries) == 0 {
		sb.WriteString("No commands are available.\n")
	}

	sb.WriteString("Any other command will be blocked (exit code 127).\n")

	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: sb.String()}},
	}, nil
}

func errorResult(msg string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: msg}},
		IsError: true,
	}
}
