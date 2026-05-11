package adapter

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/bornholm/leash/pkg/builtin"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func ToolToBuiltin(serverName string, tool *mcp.Tool, session *mcp.ClientSession) *builtin.Builtin {
	builtinName := sanitizeName(serverName) + "_" + sanitizeName(tool.Name)

	schema := parseInputSchema(tool.InputSchema)
	args, flags := schemaToArgsDefs(schema)

	if !schemaHasProperty(schema, "help") && !schemaHasProperty(schema, "h") {
		flags = append(flags, builtin.FlagDef{
			Name:        "help",
			Short:       "h",
			Default:     "",
			Description: "Afficher l'aide de cette commande",
		})
	}

	b := builtin.New(builtinName).
		Description(tool.Description).
		Category(serverName)

	for _, a := range args {
		b = b.Arg(a.Name, a.Description, a.Required)
	}
	for _, f := range flags {
		b = b.Flag(f.Name, f.Short, f.Default, f.Description)
	}

	capturedArgs := args
	capturedFlags := flags
	capturedTool := tool
	capturedSchema := schema

	return b.Handle(func(ctx context.Context, c *builtin.Call) error {
		if c.Flags["help"] != "" {
			return printHelp(c, builtinName, capturedTool, capturedArgs, capturedFlags, capturedSchema)
		}

		arguments := map[string]any{}

		for i, argDef := range capturedArgs {
			if i < len(c.Args) {
				arguments[argDef.Name] = c.Args[i]
			}
		}

		for _, name := range capturedSchema.Required {
			prop, exists := capturedSchema.Properties[name]
			if !exists {
				continue
			}
			if !isNonScalarType(prop.Type) {
				continue
			}
			if _, exists := arguments[name]; exists {
				continue
			}
			arguments[name] = map[string]any{}
		}

		for name, val := range c.Flags {
			if name == "help" {
				continue
			}
			if val != "" {
				prop, exists := capturedSchema.Properties[name]
				if exists && isNonScalarType(prop.Type) {
					var parsed any
					if err := json.Unmarshal([]byte(val), &parsed); err != nil {
						return fmt.Errorf("flag %s: invalid JSON: %w", name, err)
					}
					arguments[name] = parsed
				} else {
					arguments[name] = val
				}
			}
		}

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
			return builtin.ExitError{Code: 1}
		}
		fmt.Fprint(c.Stdout, text)
		return nil
	})
}

func printHelp(c *builtin.Call, builtinName string, tool *mcp.Tool, args []builtin.ArgDef, flags []builtin.FlagDef, schema inputSchema) error {

	if tool.Description != "" {
		fmt.Fprintf(c.Stdout, "%s\n\n", tool.Description)
	}
	fmt.Fprintf(c.Stdout, "Usage: %s", builtinName)
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

	if len(flags) > 0 || hasRequiredNonScalar(schema, args) {
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
			prop, hasProp := schema.Properties[f.Name]

			jsonHint := ""
			if hasProp && isNonScalarType(prop.Type) {
				jsonHint = " " + formatSchemaHint(prop)
			}
			fmt.Fprintf(c.Stdout, "  %-24s %s%s%s\n", nameWithShort, f.Description, jsonHint, def)
		}
		for _, name := range schema.Required {
			prop, exists := schema.Properties[name]
			if !exists || !isNonScalarType(prop.Type) {
				continue
			}
			if argNameExists(name, args) {
				continue
			}
			schemaHint := formatSchemaHint(prop)
			fmt.Fprintf(c.Stdout, "  %-24s %s %s (requis)\n", "--"+name, prop.Description, schemaHint)
		}
	}
	return nil
}

func hasRequiredNonScalar(schema inputSchema, args []builtin.ArgDef) bool {
	for _, name := range schema.Required {
		prop, exists := schema.Properties[name]
		if !exists {
			continue
		}
		if isNonScalarType(prop.Type) && !argNameExists(name, args) {
			return true
		}
	}
	return false
}

func argNameExists(name string, args []builtin.ArgDef) bool {
	for _, a := range args {
		if a.Name == name {
			return true
		}
	}
	return false
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
	Description string                    `json:"description"`
	Type        string                    `json:"type"`
	Properties  map[string]propertySchema `json:"properties"`
	Items       *propertySchema           `json:"items"`
	Required    []string                  `json:"required"`
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
	if err := json.Unmarshal(data, &s); err != nil {
		return inputSchema{}
	}
	return s
}

func schemaToArgsDefs(s inputSchema) (args []builtin.ArgDef, flags []builtin.FlagDef) {
	requiredSet := make(map[string]bool, len(s.Required))
	for _, name := range s.Required {
		requiredSet[name] = true
	}

	for _, name := range s.Required {
		prop := s.Properties[name]
		if isNonScalarType(prop.Type) {
			continue
		}
		args = append(args, builtin.ArgDef{
			Name:        name,
			Description: prop.Description,
			Required:    true,
		})
	}

	for name, prop := range s.Properties {
		if requiredSet[name] {
			continue
		}
		flags = append(flags, builtin.FlagDef{
			Name:        name,
			Short:       "",
			Default:     "",
			Description: prop.Description,
		})
	}

	for _, name := range s.Required {
		prop, exists := s.Properties[name]
		if !exists {
			continue
		}
		if !isNonScalarType(prop.Type) {
			continue
		}
		flags = append(flags, builtin.FlagDef{
			Name:        name,
			Short:       "",
			Default:     "",
			Description: prop.Description + " " + formatSchemaHint(prop),
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

func isScalarType(t string) bool {
	if t == "" {
		return true
	}
	return t == "string" || t == "number" || t == "integer" || t == "boolean"
}

func isNonScalarType(t string) bool {
	return t == "object" || t == "array"
}

func formatSchemaHint(prop propertySchema) string {
	return formatProp(prop, false)
}

func formatProp(prop propertySchema, nested bool) string {
	var sb strings.Builder

	if prop.Type == "object" && len(prop.Properties) > 0 {
		grouped := groupDottedProperties(prop.Properties)

		sb.WriteString("{")
		keys := make([]string, 0, len(grouped))
		for k := range grouped {
			keys = append(keys, k)
		}
		for i, k := range keys {
			if i > 0 {
				sb.WriteString(", ")
			}
			sb.WriteString(k)
			sb.WriteString(": ")
			sb.WriteString(formatProp(grouped[k], true))
		}
		sb.WriteString("}")
		return sb.String()
	}

	if prop.Type == "array" && prop.Items != nil {
		sb.WriteString("[")
		if prop.Items.Type == "object" && len(prop.Items.Properties) > 0 {
			grouped := groupDottedProperties(prop.Items.Properties)
			sb.WriteString("{")
			keys := make([]string, 0, len(grouped))
			for k := range grouped {
				keys = append(keys, k)
			}
			for i, k := range keys {
				if i > 0 {
					sb.WriteString(", ")
				}
				sb.WriteString(k)
				sb.WriteString(": ")
				sb.WriteString(formatProp(grouped[k], true))
			}
			sb.WriteString("}")
		} else {
			sb.WriteString(prop.Items.Type)
		}
		sb.WriteString("]")
		return sb.String()
	}

	return prop.Type
}

func groupDottedProperties(props map[string]propertySchema) map[string]propertySchema {
	result := make(map[string]propertySchema)

	for name, prop := range props {
		parts := strings.SplitN(name, ".", 2)
		if len(parts) == 2 {
			parent, child := parts[0], parts[1]
			if existing, ok := result[parent]; ok {
				if existing.Properties == nil {
					existing.Properties = make(map[string]propertySchema)
				}
				existing.Properties[child] = prop
				existing.Type = "object"
				result[parent] = existing
			} else {
				result[parent] = propertySchema{
					Type: "object",
					Properties: map[string]propertySchema{
						child: prop,
					},
				}
			}
		} else {
			result[name] = prop
		}
	}

	return result
}
