package mcp

import (
	"github.com/bornholm/leash/pkg/skill"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// skillToMCPTool convertit un Skill en *mcp.Tool pour le SDK officiel.
// Le nom du tool MCP est préfixé par "skill_" pour éviter les conflits.
func skillToMCPTool(sk *skill.Skill) *mcp.Tool {
	properties := map[string]any{}
	var required []string

	for _, arg := range sk.Args {
		prop := map[string]any{
			"type":        "string",
			"description": arg.Description,
		}
		properties[arg.Name] = prop
		if arg.Required {
			required = append(required, arg.Name)
		}
	}

	for _, flag := range sk.Flags {
		desc := flag.Description
		if flag.Default != "" {
			desc += " (default: " + flag.Default + ")"
		}
		properties[flag.Name] = map[string]any{
			"type":        "string",
			"description": desc,
		}
	}

	schema := map[string]any{
		"type":       "object",
		"properties": properties,
	}
	if len(required) > 0 {
		schema["required"] = required
	}

	return &mcp.Tool{
		Name:        "skill_" + sk.Name,
		Description: sk.Description,
		InputSchema: schema,
	}
}
