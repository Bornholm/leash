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
			sk := adapter.ToolToBuiltin(server.Name, tool, server.Session)
			if regErr := reg.Register(sk); regErr != nil {
				slog.WarnContext(ctx, "impossible d'enregistrer le tool MCP (doublon ?), il sera ignoré",
					"server", server.Name,
					"tool", tool.Name,
					"builtin", sk.Name,
					"error", regErr,
				)
			}
		}
	}

	return servers, nil
}
