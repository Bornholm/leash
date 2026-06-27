package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
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
			Instructions: eng.Instructions(),
		},
	)
	ms := &MCPServer{eng: eng, server: s}
	ms.registerTools()
	return ms
}

// ServeStdio démarre le serveur MCP sur stdin/stdout.
func (ms *MCPServer) ServeStdio() error {
	return ms.server.Run(context.Background(), &mcp.StdioTransport{})
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

// handleExecuteShell trace la réponse (ou l'erreur) effectivement renvoyée à
// l'agent pour l'unique tool MCP exposé — y compris les erreurs propres au
// tool (paramètres invalides, script manquant) qui ne passent jamais par
// l'Engine et ne sont donc jamais vues par l'audit log.
func (ms *MCPServer) handleExecuteShell(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	var args map[string]any
	if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
		slog.WarnContext(ctx, "mcp: execute_shell tool error: invalid parameters", "error", err)
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
		slog.WarnContext(ctx, "mcp: execute_shell tool error: missing script parameter")
		return errorResult("parameter 'script' is required"), nil
	}

	result, err := ms.eng.Exec(context.Background(), script)
	if err != nil {
		slog.ErrorContext(ctx, "mcp: execute_shell tool error: execution failed", "error", err)
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

	response := sb.String()
	slog.DebugContext(ctx, "mcp: execute_shell tool response",
		"is_error", result.ExitCode != 0,
		"exit_code", result.ExitCode,
		"response", response,
	)

	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: response}},
		IsError: result.ExitCode != 0,
	}, nil
}

func errorResult(msg string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: msg}},
		IsError: true,
	}
}
