package mcpregistry

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/bornholm/leash/internal/mcp/adapter"
	"github.com/bornholm/leash/internal/mcp/client"
	"github.com/bornholm/leash/internal/registry"
	"github.com/bornholm/leash/internal/security"
)

// LoadFromConfig connecte tous les serveurs MCP configurés, convertit leurs tools en skills
// et les enregistre dans le registry. Retourne les serveurs connectés pour leur Close() en fin de vie.
func LoadFromConfig(
	ctx context.Context,
	cfgs []security.MCPServerConfig,
	reg *registry.Registry,
) ([]*client.ConnectedServer, error) {
	servers, err := client.ConnectAll(ctx, cfgs)
	if err != nil {
		return nil, fmt.Errorf("connexion aux serveurs MCP : %w", err)
	}

	for _, server := range servers {
		for _, tool := range server.Tools {
			sk := adapter.ToolToSkill(server.Name, tool, server.Session)
			if regErr := reg.Register(sk); regErr != nil {
				slog.WarnContext(ctx, "impossible d'enregistrer le tool MCP (doublon ?), il sera ignoré",
					"server", server.Name,
					"tool", tool.Name,
					"skill", sk.Name,
					"error", regErr,
				)
			}
		}
	}

	return servers, nil
}
