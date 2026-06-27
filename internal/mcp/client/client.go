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
	"time"

	"github.com/bornholm/leash/internal/security"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// defaultMCPConnectTimeout borne la connexion à un serveur MCP externe
// (handshake + premier ListTools) lorsque cfg.Timeout n'est pas renseigné.
// Sans cette borne, un serveur injoignable (DNS qui ne répond pas, port
// fermé en silence, sous-processus stdio qui ne se termine jamais) bloque
// indéfiniment Connect, et donc tout leash.New qui en dépend.
const defaultMCPConnectTimeout = 10 * time.Second

// ConnectedServer représente une connexion active à un serveur MCP externe.
type ConnectedServer struct {
	Name    string
	Session *mcp.ClientSession
	Tools   []*mcp.Tool
}

// Connect ouvre une session vers un serveur MCP configuré et récupère la liste de ses tools.
func Connect(ctx context.Context, cfg security.MCPServerConfig) (*ConnectedServer, error) {
	timeout := cfg.Timeout.Duration
	if timeout <= 0 {
		timeout = defaultMCPConnectTimeout
	}
	// On borne la durée du handshake (Connect + premier ListTools) sans
	// annuler le contexte une fois la session établie : la session (flux SSE
	// long-lived, sous-processus stdio, etc.) doit rester valide bien après
	// le retour de cette fonction. Un context.WithTimeout classique avec
	// defer cancel() annulerait le contexte dès que Connect() retourne,
	// succès ou pas, ce qui coupe immédiatement la connexion sous-jacente —
	// timer.Stop() empêche cancel() de se déclencher une fois la connexion
	// établie ; il ne s'exécute que si le handshake dépasse réellement timeout.
	ctx, cancel := context.WithCancel(ctx)
	timer := time.AfterFunc(timeout, cancel)
	defer timer.Stop()

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
		if closeErr := session.Close(); closeErr != nil {
			slog.Warn("mcp client: error closing session after ListTools failure", "server", cfg.Name, "error", closeErr)
		}
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
