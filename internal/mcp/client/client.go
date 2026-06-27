package client

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
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
//
// Le go-sdk MCP détache délibérément le contexte passé à transport.Connect via
// xcontext.Detach, de sorte que son contexte de connexion interne (connCtx)
// n'est pas annulé lorsque notre ctx est annulé. Par conséquent, un
// context.WithTimeout ou un time.AfterFunc sur ctx n'interrompt pas les dials
// TCP en cours. De plus, le sdk envoie un DELETE HTTP lors du Close() sur le
// chemin d'erreur (via connCtx), ce qui produit un second dial.
//
// Pour garantir un retour dans les délais configurés indépendamment de ces
// détails internes, on utilise le pattern goroutine + select : l'opération
// entière tourne en arrière-plan et on sélectionne le premier résultat.
// Le Dialer.Timeout sur le transport HTTP borne la durée de vie du goroutine
// en arrière-plan en cas d'adresse injoignable (évite la fuite indéfinie).
func Connect(ctx context.Context, cfg security.MCPServerConfig) (*ConnectedServer, error) {
	timeout := cfg.Timeout.Duration
	if timeout <= 0 {
		timeout = defaultMCPConnectTimeout
	}

	type connectResult struct {
		server *ConnectedServer
		err    error
	}
	// Buffer de 1 pour que le goroutine ne bloque pas si on est déjà partis
	// (timeout ou annulation du ctx parent).
	ch := make(chan connectResult, 1)

	go func() {
		server, err := doConnect(ctx, cfg, timeout)
		ch <- connectResult{server, err}
	}()

	select {
	case r := <-ch:
		return r.server, r.err
	case <-time.After(timeout):
		return nil, fmt.Errorf("serveur MCP %q : délai de connexion dépassé (%v)", cfg.Name, timeout)
	case <-ctx.Done():
		return nil, fmt.Errorf("serveur MCP %q : %w", cfg.Name, ctx.Err())
	}
}

// doConnect effectue la connexion MCP réelle. Il est toujours exécuté dans
// un goroutine dédié (cf. Connect) ; ses erreurs sont retournées via un canal.
func doConnect(ctx context.Context, cfg security.MCPServerConfig, timeout time.Duration) (*ConnectedServer, error) {
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
			HTTPClient: buildHTTPClient(cfg.Headers, timeout),
		}
	case "sse":
		if cfg.URL == "" {
			return nil, fmt.Errorf("serveur MCP %q : 'url' est requis pour le transport sse", cfg.Name)
		}
		transport = &mcp.SSEClientTransport{
			Endpoint:   cfg.URL,
			HTTPClient: buildHTTPClient(cfg.Headers, timeout),
		}
	default:
		return nil, fmt.Errorf("serveur MCP %q : transport inconnu %q (attendu: stdio, http, sse)", cfg.Name, cfg.Transport)
	}

	client := mcp.NewClient(&mcp.Implementation{Name: "leash", Version: "1.0.0"}, nil)
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		if stderrBuf != nil {
			if stderrStr := stderrBuf.String(); stderrStr != "" {
				return nil, fmt.Errorf("connexion au serveur MCP %q : %w ; stderr: %s", cfg.Name, err, stderrStr)
			}
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

// buildHTTPClient construit un http.Client avec injection optionnelle d'entêtes fixes
// et un Dialer borné par dialTimeout. Le timeout Dialer agit au niveau TCP —
// il ne s'applique qu'à l'établissement de la connexion, pas aux échanges
// ultérieurs — ce qui préserve les sessions SSE/HTTP long-lived. Il borne
// également la durée de vie du goroutine de connexion en arrière-plan lorsque
// Connect() dépasse son délai (pattern goroutine + select dans Connect).
func buildHTTPClient(headers map[string]string, dialTimeout time.Duration) *http.Client {
	dialer := &net.Dialer{
		Timeout:   dialTimeout,
		KeepAlive: 30 * time.Second,
	}
	base := &http.Transport{
		DialContext:         dialer.DialContext,
		TLSHandshakeTimeout: dialTimeout,
	}
	if len(headers) == 0 {
		return &http.Client{Transport: base}
	}
	return &http.Client{
		Transport: &headerTransport{
			wrapped: base,
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
