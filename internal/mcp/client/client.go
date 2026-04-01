package client

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"

	"github.com/bornholm/leash/internal/security"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ConnectedServer représente une connexion active à un serveur MCP externe.
type ConnectedServer struct {
	Name    string
	Session *mcp.ClientSession
	Tools   []*mcp.Tool
}

// Connect ouvre une session vers un serveur MCP configuré et récupère la liste de ses tools.
func Connect(ctx context.Context, cfg security.MCPServerConfig) (*ConnectedServer, error) {
	var transport mcp.Transport
	var stderrBuf *bytes.Buffer
	switch cfg.Transport {
	case "stdio":
		if len(cfg.Command) == 0 {
			return nil, fmt.Errorf("serveur MCP %q : 'command' est requis pour le transport stdio", cfg.Name)
		}
		cmd := exec.Command(cfg.Command[0], cfg.Command[1:]...) //nolint:gosec
		if len(cfg.Env) > 0 {
			cmd.Env = os.Environ()
			for k, v := range cfg.Env {
				cmd.Env = append(cmd.Env, k+"="+v)
			}
		}
		stderrPipe, err := cmd.StderrPipe()
		if err != nil {
			return nil, fmt.Errorf("serveur MCP %q : impossible de créer le pipe stderr : %w", cfg.Name, err)
		}
		stderrBuf = &bytes.Buffer{}
		go func() {
			_, _ = io.Copy(stderrBuf, stderrPipe)
		}()
		transport = &mcp.CommandTransport{Command: cmd}
	case "http", "":
		if cfg.URL == "" {
			return nil, fmt.Errorf("serveur MCP %q : 'url' est requis pour le transport http", cfg.Name)
		}
		transport = &mcp.StreamableClientTransport{
			Endpoint:   cfg.URL,
			HTTPClient: buildHTTPClient(cfg.Headers),
		}
	case "sse":
		if cfg.URL == "" {
			return nil, fmt.Errorf("serveur MCP %q : 'url' est requis pour le transport sse", cfg.Name)
		}
		transport = &mcp.SSEClientTransport{
			Endpoint:   cfg.URL,
			HTTPClient: buildHTTPClient(cfg.Headers),
		}
	default:
		return nil, fmt.Errorf("serveur MCP %q : transport inconnu %q (attendu: stdio, http, sse)", cfg.Name, cfg.Transport)
	}

	client := mcp.NewClient(&mcp.Implementation{Name: "leash", Version: "1.0.0"}, nil)
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		stderrStr := stderrBuf.String()
		if stderrStr != "" {
			return nil, fmt.Errorf("connexion au serveur MCP %q : %w ; stderr: %s", cfg.Name, err, stderrStr)
		}
		return nil, fmt.Errorf("connexion au serveur MCP %q : %w", cfg.Name, err)
	}

	result, err := session.ListTools(ctx, &mcp.ListToolsParams{})
	if err != nil {
		_ = session.Close()
		return nil, fmt.Errorf("listage des tools du serveur MCP %q : %w", cfg.Name, err)
	}

	return &ConnectedServer{
		Name:    cfg.Name,
		Session: session,
		Tools:   result.Tools,
	}, nil
}

// buildHTTPClient construit un http.Client avec injection optionnelle d'entêtes fixes.
func buildHTTPClient(headers map[string]string) *http.Client {
	if len(headers) == 0 {
		return &http.Client{}
	}
	return &http.Client{
		Transport: &headerTransport{
			wrapped: http.DefaultTransport,
			headers: headers,
		},
	}
}

// headerTransport est un http.RoundTripper qui injecte des entêtes fixes dans chaque requête.
type headerTransport struct {
	wrapped http.RoundTripper
	headers map[string]string
}

func (t *headerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Cloner la requête pour ne pas modifier l'originale.
	r := req.Clone(req.Context())
	for k, v := range t.headers {
		r.Header.Set(k, v)
	}
	return t.wrapped.RoundTrip(r)
}

// ConnectAll connecte tous les serveurs MCP configurés.
// En cas d'échec sur un serveur individuel, un avertissement est loggé et le serveur est ignoré (mode dégradé).
func ConnectAll(ctx context.Context, cfgs []security.MCPServerConfig) ([]*ConnectedServer, error) {
	var connected []*ConnectedServer
	for _, cfg := range cfgs {
		server, err := Connect(ctx, cfg)
		if err != nil {
			slog.WarnContext(ctx, "impossible de connecter le serveur MCP, il sera ignoré",
				"server", cfg.Name, "error", err)
			continue
		}
		slog.InfoContext(ctx, "serveur MCP connecté",
			"server", cfg.Name, "tools", len(server.Tools))
		connected = append(connected, server)
	}
	return connected, nil
}
