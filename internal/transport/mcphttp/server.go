package mcphttp

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/bornholm/leash/pkg/leash"
)

// Server expose le Manager de workspaces via le transport MCP Streamable
// HTTP du SDK officiel, avec authentification Bearer et résolution du
// discriminant en hash HMAC.
type Server struct {
	cfg     *ServerConfig
	mgr     *Manager
	logger  *slog.Logger
	handler http.Handler
	rl      *ipRateLimiter

	stopCh chan struct{}
	wg     sync.WaitGroup
}

type requestIDKey struct{}

// NewServer construit le serveur HTTP MCP et sa chaîne middleware :
//
//	rate-limit → request-ID → security-headers → healthz / (max-bytes → auth → MCP)
func NewServer(cfg *ServerConfig, mgr *Manager) *Server {
	s := &Server{
		cfg:    cfg,
		mgr:    mgr,
		logger: slog.Default(),
		stopCh: make(chan struct{}),
	}

	mcpHandler := mcp.NewStreamableHTTPHandler(s.getServer, &mcp.StreamableHTTPOptions{})

	// Couche auth + limite corps, appliquées uniquement aux routes MCP.
	var mcpRoute http.Handler = mcpHandler
	if cfg.MaxRequestBodyBytes > 0 {
		mcpRoute = http.MaxBytesHandler(mcpRoute, cfg.MaxRequestBodyBytes)
	}
	mcpRoute = authMiddleware(cfg.APIKeys, mcpRoute)

	mux := http.NewServeMux()
	mux.Handle("GET /healthz", http.HandlerFunc(healthzHandler))
	s.registerFileRoutes(mux)
	mux.Handle("/", mcpRoute)

	// Middlewares communs à toutes les routes (y compris healthz).
	var h http.Handler = mux
	h = securityHeadersMiddleware(h)
	h = requestIDMiddleware(h)

	if cfg.HTTPRateLimit > 0 {
		s.rl = newIPRateLimiter(cfg.HTTPRateLimit, cfg.HTTPBurst)
		h = httpRateLimitMiddleware(s.rl, cfg.TrustProxyHeaders, h)

		s.wg.Go(func() {
			ticker := time.NewTicker(5 * time.Minute)
			defer ticker.Stop()
			for {
				select {
				case <-s.stopCh:
					return
				case <-ticker.C:
					s.rl.cleanup(10 * time.Minute)
				}
			}
		})
	}

	s.handler = h
	return s
}

// Shutdown arrête les goroutines internes du serveur (reaper rate-limiter).
// À appeler après httpServer.Shutdown().
func (s *Server) Shutdown() {
	close(s.stopCh)
	s.wg.Wait()
}

// ServeHTTP implémente http.Handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.handler.ServeHTTP(w, r)
}

func healthzHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

func requestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-ID")
		if id == "" {
			generated, err := generateRequestID()
			if err != nil {
				http.Error(w, "internal error", http.StatusInternalServerError)
				return
			}
			id = generated
		}
		w.Header().Set("X-Request-ID", id)
		ctx := context.WithValue(r.Context(), requestIDKey{}, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func requestIDFromContext(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey{}).(string)
	return id
}

func generateRequestID() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("mcphttp: generating request ID: %w", err)
	}
	return hex.EncodeToString(b), nil
}

func securityHeadersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}

// resolveDiscriminant dérive le hash de workspace pour une requête donnée.
func resolveDiscriminant(r *http.Request, cfg *ServerConfig, key *APIKeyConfig) (string, error) {
	if key != nil && key.WorkspaceID != "" {
		return hashDiscriminator(cfg.hmacSecret, key.WorkspaceID)
	}

	var raw string
	if cfg.DiscHeader != "" {
		raw = r.Header.Get(cfg.DiscHeader)
	}
	if raw == "" && cfg.DiscURLParam != "" {
		raw = r.PathValue(cfg.DiscURLParam)
		if raw == "" {
			raw = r.URL.Query().Get(cfg.DiscURLParam)
		}
	}
	if raw == "" {
		return "", ErrEmptyDiscriminator
	}
	return hashDiscriminator(cfg.hmacSecret, raw)
}

// getServer est appelé par le StreamableHTTPHandler pour chaque nouvelle
// session. Retourner nil entraîne un 400 Bad Request.
func (s *Server) getServer(r *http.Request) *mcp.Server {
	key, ok := apiKeyFromContext(r.Context())
	if !ok {
		s.logger.Error("mcphttp: missing API key in request context")
		return nil
	}

	id, err := resolveDiscriminant(r, s.cfg, key)
	ephemeral := false
	if err != nil {
		if !errors.Is(err, ErrEmptyDiscriminator) {
			s.logger.Warn("mcphttp: resolving discriminant", "error", err,
				"request_id", requestIDFromContext(r.Context()))
			return nil
		}
		eid, genErr := generateEphemeralID()
		if genErr != nil {
			s.logger.Error("mcphttp: generating ephemeral workspace ID", "error", genErr,
				"request_id", requestIDFromContext(r.Context()))
			return nil
		}
		id = eid
		ephemeral = true
		s.logger.Debug("mcphttp: no discriminant — ephemeral workspace",
			"workspace_id", id, "request_id", requestIDFromContext(r.Context()))
	}

	ws, err := s.mgr.Acquire(r.Context(), id, key)
	if err != nil {
		s.logger.Error("mcphttp: acquiring workspace", "error", err, "workspace", id,
			"request_id", requestIDFromContext(r.Context()))
		return nil
	}

	srvOpts := &mcp.ServerOptions{
		Instructions: ws.engine.Instructions(),
	}

	if ephemeral {
		// InitializedHandler receives the *ServerSession, which exposes
		// Wait() — the only SDK signal that reliably outlives the initial HTTP
		// request and fires exactly when the MCP session ends.
		ephemeralID := id
		srvOpts.InitializedHandler = func(_ context.Context, req *mcp.InitializedRequest) {
			sess := req.Session
			s.wg.Go(func() {
				waitDone := make(chan struct{})
				go func() {
					defer close(waitDone)
					_ = sess.Wait()
				}()
				select {
				case <-waitDone:
				case <-s.stopCh:
					_ = sess.Close()
					<-waitDone
				}
				s.mgr.Evict(ephemeralID)
			})
		}
	}

	srv := mcp.NewServer(&mcp.Implementation{Name: "LeaSH", Version: "1.0.0"}, srvOpts)
	registerExecuteShell(srv, ws)
	return srv
}

// generateEphemeralID produit un identifiant unique pour un workspace éphémère.
func generateEphemeralID() (string, error) {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("mcphttp: generating ephemeral workspace ID: %w", err)
	}
	return "eph-" + hex.EncodeToString(b), nil
}

func registerExecuteShell(srv *mcp.Server, ws *Workspace) {
	srv.AddTool(
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
		func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return handleExecuteShell(ctx, req, ws)
		},
	)
}

func handleExecuteShell(ctx context.Context, req *mcp.CallToolRequest, ws *Workspace) (*mcp.CallToolResult, error) {
	var args map[string]any
	if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
		slog.WarnContext(ctx, "mcphttp: execute_shell tool error: invalid parameters",
			"workspace_id", ws.id, "api_key", ws.apiKey,
			"request_id", requestIDFromContext(ctx),
			"error", err)
		return errorResult("invalid parameters"), nil
	}

	script, _ := args["script"].(string)
	if script == "" {
		slog.WarnContext(ctx, "mcphttp: execute_shell tool error: missing script parameter",
			"workspace_id", ws.id, "api_key", ws.apiKey,
			"request_id", requestIDFromContext(ctx))
		return errorResult("parameter 'script' is required"), nil
	}

	var stdout, stderr strings.Builder
	result, err := ws.Exec(ctx, script, nil, &stdout, &stderr)
	if err != nil {
		slog.ErrorContext(ctx, "mcphttp: execute_shell tool error: execution failed",
			"workspace_id", ws.id, "api_key", ws.apiKey,
			"request_id", requestIDFromContext(ctx),
			"error", err)
		return errorResult("execution failed"), nil
	}

	response := formatExecResult(result, stdout.String(), stderr.String())
	slog.DebugContext(ctx, "mcphttp: execute_shell tool response",
		"workspace_id", ws.id,
		"api_key", ws.apiKey,
		"request_id", requestIDFromContext(ctx),
		"is_error", result.ExitCode != 0,
		"exit_code", result.ExitCode,
	)

	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: response}},
		IsError: result.ExitCode != 0,
	}, nil
}

func formatExecResult(result *leash.ExecResult, stdout, stderr string) string {
	var sb strings.Builder
	if stdout != "" {
		sb.WriteString("## STDOUT\n")
		sb.WriteString(stdout)
		sb.WriteString("\n")
	}
	if stderr != "" {
		sb.WriteString("## STDERR\n")
		sb.WriteString(stderr)
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

	return sb.String()
}

func errorResult(msg string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: msg}},
		IsError: true,
	}
}
