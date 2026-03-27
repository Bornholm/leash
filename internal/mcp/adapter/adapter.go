package adapter

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/bornholm/leash/pkg/skill"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ToolToSkill convertit un MCP Tool en *skill.Skill prêt à être enregistré.
// Le nom du skill est "<serverName>_<toolName>" (les -, . et / sont remplacés par _).
func ToolToSkill(serverName string, tool *mcp.Tool, session *mcp.ClientSession) *skill.Skill {
	skillName := sanitizeName(serverName) + "_" + sanitizeName(tool.Name)

	schema := parseInputSchema(tool.InputSchema)
	args, flags := schemaToArgsDefs(schema)

	// Ajouter --help automatiquement si le schema n'a pas déjà une propriété "help" ou "h"
	if !schemaHasProperty(schema, "help") && !schemaHasProperty(schema, "h") {
		flags = append(flags, skill.FlagDef{
			Name:        "help",
			Short:       "h",
			Default:     "",
			Description: "Afficher l'aide de cette commande",
		})
	}

	b := skill.New(skillName).
		Description(tool.Description).
		Category(serverName)

	for _, a := range args {
		b = b.Arg(a.Name, a.Description, a.Required)
	}
	for _, f := range flags {
		b = b.Flag(f.Name, f.Short, f.Default, f.Description)
	}

	// Capture les valeurs pour la closure
	capturedArgs := args
	capturedFlags := flags
	capturedTool := tool
	capturedSchema := schema

	return b.Handle(func(ctx context.Context, c *skill.Call) error {
		// Gestion de --help
		if c.Flags["help"] != "" {
			return printHelp(c, skillName, capturedTool, capturedArgs, capturedFlags)
		}

		arguments := map[string]any{}

		// Mapper les args positionnels → nom de propriété du schema
		for i, argDef := range capturedArgs {
			if i < len(c.Args) {
				arguments[argDef.Name] = c.Args[i]
			}
		}

		// Mapper les flags (ignorer "help" qui est notre flag interne)
		for name, val := range c.Flags {
			if name == "help" {
				continue
			}
			if val != "" {
				arguments[name] = val
			}
		}

		// Injection depuis stdin si le schema attend une propriété stdin/input non fournie
		if stdinKey := stdinPropertyName(capturedSchema); stdinKey != "" {
			if _, ok := arguments[stdinKey]; !ok {
				data, err := io.ReadAll(c.Stdin)
				if err != nil {
					return fmt.Errorf("lecture de stdin : %w", err)
				}
				if len(data) > 0 {
					arguments[stdinKey] = string(data)
				}
			}
		}

		result, err := session.CallTool(ctx, &mcp.CallToolParams{
			Name:      capturedTool.Name,
			Arguments: arguments,
		})
		if err != nil {
			return fmt.Errorf("appel MCP %s : %w", capturedTool.Name, err)
		}

		text := extractText(result.Content)
		if result.IsError {
			fmt.Fprint(c.Stderr, text)
			return skill.ExitError{Code: 1}
		}
		fmt.Fprint(c.Stdout, text)
		return nil
	})
}

// printHelp affiche l'aide d'un tool MCP sur stdout.
func printHelp(c *skill.Call, skillName string, tool *mcp.Tool, args []skill.ArgDef, flags []skill.FlagDef) error {
	if tool.Description != "" {
		fmt.Fprintf(c.Stdout, "%s\n\n", tool.Description)
	}
	fmt.Fprintf(c.Stdout, "Usage: %s", skillName)
	for _, a := range args {
		if a.Required {
			fmt.Fprintf(c.Stdout, " <%s>", a.Name)
		} else {
			fmt.Fprintf(c.Stdout, " [%s]", a.Name)
		}
	}
	fmt.Fprint(c.Stdout, "\n")

	if len(args) > 0 {
		fmt.Fprint(c.Stdout, "\nArguments:\n")
		for _, a := range args {
			req := ""
			if a.Required {
				req = " (requis)"
			}
			fmt.Fprintf(c.Stdout, "  %-20s %s%s\n", a.Name, a.Description, req)
		}
	}

	if len(flags) > 0 {
		fmt.Fprint(c.Stdout, "\nFlags:\n")
		for _, f := range flags {
			nameWithShort := "--" + f.Name
			if f.Short != "" {
				nameWithShort += ", -" + f.Short
			}
			def := ""
			if f.Default != "" {
				def = fmt.Sprintf(" (défaut: %s)", f.Default)
			}
			fmt.Fprintf(c.Stdout, "  %-24s %s%s\n", nameWithShort, f.Description, def)
		}
	}
	return nil
}

// extractText concatène le texte de tous les TextContent dans la liste.
func extractText(contents []mcp.Content) string {
	var sb strings.Builder
	for _, c := range contents {
		if tc, ok := c.(*mcp.TextContent); ok {
			sb.WriteString(tc.Text)
		}
	}
	return sb.String()
}

// sanitizeName remplace les caractères non-shell (-, ., /) par _.
func sanitizeName(s string) string {
	return strings.NewReplacer("-", "_", ".", "_", "/", "_").Replace(s)
}

// inputSchema est une représentation partielle du JSON Schema d'un tool MCP.
type inputSchema struct {
	Properties map[string]propertySchema `json:"properties"`
	Required   []string                  `json:"required"`
}

type propertySchema struct {
	Description string `json:"description"`
	Type        string `json:"type"`
}

// parseInputSchema convertit le champ InputSchema (any) en inputSchema structuré.
func parseInputSchema(raw any) inputSchema {
	if raw == nil {
		return inputSchema{}
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return inputSchema{}
	}
	var s inputSchema
	_ = json.Unmarshal(data, &s)
	return s
}

// schemaToArgsDefs convertit un schema en ArgDef (requis) et FlagDef (optionnels).
func schemaToArgsDefs(s inputSchema) (args []skill.ArgDef, flags []skill.FlagDef) {
	requiredSet := make(map[string]bool, len(s.Required))
	for _, name := range s.Required {
		requiredSet[name] = true
	}

	// Les args positionnels sont dans l'ordre de required
	for _, name := range s.Required {
		prop := s.Properties[name]
		args = append(args, skill.ArgDef{
			Name:        name,
			Description: prop.Description,
			Required:    true,
		})
	}

	// Les flags sont les propriétés optionnelles (pas dans required)
	for name, prop := range s.Properties {
		if requiredSet[name] {
			continue
		}
		flags = append(flags, skill.FlagDef{
			Name:        name,
			Short:       "",
			Default:     "",
			Description: prop.Description,
		})
	}

	return args, flags
}

// schemaHasProperty retourne true si le schema a une propriété avec ce nom.
func schemaHasProperty(s inputSchema, name string) bool {
	_, ok := s.Properties[name]
	return ok
}

// stdinPropertyName retourne le nom de la propriété stdin/input dans le schema, ou "".
func stdinPropertyName(s inputSchema) string {
	for _, name := range []string{"stdin", "input"} {
		if _, ok := s.Properties[name]; ok {
			return name
		}
	}
	return ""
}
