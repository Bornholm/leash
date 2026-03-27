package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/bornholm/leash/internal/engine"
	"github.com/bornholm/leash/pkg/skill"
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
// registered skills and available tools.
func buildInstructions(eng engine.Engine) string {
	var sb strings.Builder

	sb.WriteString(`LeaSH is a policy-enforced sandbox that lets you run shell commands and skills safely.
Scripts that violate the active policy are blocked and return exit code 127.

## Tools

### execute_shell
Run an arbitrary shell script. Use this for ad-hoc commands, pipelines, or when no skill matches your intent.
The response contains sections ## STDOUT, ## STDERR, ## EXIT CODE, and ## BLOCKED (if commands were blocked).

### list_commands
Returns full documentation for every registered skill. Call this when you are unsure of a skill's exact arguments or flags.

### skill_<name>
One dedicated tool per registered skill. Prefer these over execute_shell when a matching skill exists — they validate inputs and map directly to sandbox commands.

`)

	var skillLines []string
	eng.Registry().ForEach(func(sk *skill.Skill) {
		line := fmt.Sprintf("- **skill_%s**", sk.Name)
		if sk.Description != "" {
			line += ": " + sk.Description
		}
		if sk.Usage != "" {
			line += " — usage: `" + sk.Usage + "`"
		}
		skillLines = append(skillLines, line)
	})

	if len(skillLines) == 0 {
		sb.WriteString("## Registered skills\nNo skills are currently registered. Use execute_shell for all operations.\n")
	} else {
		sb.WriteString("## Registered skills\n")
		for _, l := range skillLines {
			sb.WriteString(l + "\n")
		}
		sb.WriteString("\nCall list_commands to get the full argument/flag documentation for any skill.\n")
	}

	sb.WriteString("\n")

	binaries := eng.Policy().AllowedBinaries()
	if len(binaries) == 0 {
		sb.WriteString("## Allowed system binaries\nNo system binaries are allowed by the active policy.\n")
	} else {
		sb.WriteString("## Allowed system binaries\n")
		for _, b := range binaries {
			sb.WriteString("- `" + b + "`\n")
		}
		sb.WriteString("\nAny other binary will be blocked (exit code 127).\n")
	}

	sb.WriteString(`
## Decision guide

1. Does a skill_* tool match what you need? → use it.
2. Need to chain multiple commands or use shell features (pipes, loops)? → use execute_shell.
3. Unsure what skills are available or how to call one? → call list_commands first.
4. A command returns exit code 127? → it was blocked by policy; do not retry it as-is.
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

	// Un tool par skill enregistré
	ms.eng.Registry().ForEach(func(sk *skill.Skill) {
		skCopy := sk
		ms.server.AddTool(skillToMCPTool(sk), ms.makeSkillHandler(skCopy))
	})
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
			sb.WriteString("\n")
		}
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: sb.String()}},
		IsError: result.ExitCode != 0,
	}, nil
}

func (ms *MCPServer) handleListCommands(_ context.Context, _ *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	var sb strings.Builder
	sb.WriteString(ms.eng.Registry().GenerateManifest())

	binaries := ms.eng.Policy().AllowedBinaries()
	if len(binaries) > 0 {
		sb.WriteString("# Allowed System Binaries\n\n")
		sb.WriteString("The following system binaries can be called directly in shell scripts:\n\n")
		for _, b := range binaries {
			sb.WriteString("- `" + b + "`\n")
		}
		sb.WriteString("\nAny other binary will be blocked (exit code 127).\n")
	} else {
		sb.WriteString("# Allowed System Binaries\n\nNo system binaries are allowed by the active policy.\n")
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: sb.String()}},
	}, nil
}

func (ms *MCPServer) makeSkillHandler(sk *skill.Skill) mcp.ToolHandler {
	return func(_ context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args map[string]any
		if len(req.Params.Arguments) > 0 {
			if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
				return errorResult("invalid parameters: " + err.Error()), nil
			}
		}

		// Build the shell script that calls the skill
		var cmdParts []string
		cmdParts = append(cmdParts, sk.Name)
		for _, arg := range sk.Args {
			if val, ok := args[arg.Name].(string); ok && val != "" {
				cmdParts = append(cmdParts, fmt.Sprintf("%q", val))
			}
		}
		for _, flag := range sk.Flags {
			if val, ok := args[flag.Name].(string); ok && val != "" && val != flag.Default {
				cmdParts = append(cmdParts, fmt.Sprintf("--%s=%q", flag.Name, val))
			}
		}
		script := strings.Join(cmdParts, " ")

		result, err := ms.eng.Exec(context.Background(), script)
		if err != nil {
			return errorResult(fmt.Sprintf("execution error: %v", err)), nil
		}

		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: string(result.Stdout)}},
			IsError: result.ExitCode != 0,
		}, nil
	}
}

func errorResult(msg string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: msg}},
		IsError: true,
	}
}
