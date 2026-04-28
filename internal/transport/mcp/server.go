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

	sb.WriteString(`LeaSH is a policy-enforced shell sandbox.

## The ONLY MCP tool is: execute_shell

There is exactly one MCP tool: **execute_shell**. Do NOT attempt to call any other name as an MCP tool.
All operations are performed by passing shell scripts to execute_shell.

## Shell commands vs MCP tools

LeaSH registers domain-specific commands as SHELL commands (not MCP tools).
These commands are invoked exclusively inside shell scripts via execute_shell.

To discover available shell commands:
  execute_shell { "script": "leash-help" }

To get help on a specific command:
  execute_shell { "script": "leash-help <command>" }
  or:
  execute_shell { "script": "<command> --help" }

`)

	// Available commands = registered commands + allowed binaries
	commands := eng.Registry().ListNames()
	binaries := eng.Policy().AllowedBinaries()

	if len(binaries) == 0 && len(commands) == 0 {
		sb.WriteString("## Available shell commands\nNo commands are currently available.\n")
	} else {
		sb.WriteString("## Available shell commands\n(These are shell commands — invoke them via execute_shell, NOT as MCP tools)\n\n")
		for _, c := range commands {
			sb.WriteString("- " + c + "\n")
		}
		for _, b := range binaries {
			sb.WriteString("- " + b + " (system binary)\n")
		}
		sb.WriteString("\nAny other command will be blocked (exit code 127).\n")
	}

	sb.WriteString(`
## Rules

1. Use execute_shell for EVERYTHING — scripts, commands, pipes, loops, etc.
2. Command blocked (exit 127)? Run execute_shell { "script": "leash-help" } to see available commands.
3. Do NOT call shell command names as MCP tools. They are shell commands only.
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

}

func (ms *MCPServer) handleExecuteShell(_ context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	var args map[string]any
	if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
		return errorResult("invalid parameters: " + err.Error()), nil
	}

	var (
		script string
		ok     bool
	)

	aliases := []string{"script", "command"}
	for _, alias := range aliases {
		script, ok = args[alias].(string)
		if ok {
			break
		}
	}

	if script == "" {
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
			sb.WriteString("\n\n→ Run execute_shell { \"script\": \"leash-help\" } to list available shell commands.\n")
		}
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: sb.String()}},
		IsError: result.ExitCode != 0,
	}, nil
}

func errorResult(msg string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: msg}},
		IsError: true,
	}
}
