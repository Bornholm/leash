// Command server démarre le serveur MCP HTTP Streaming multi-tenant de
// LeaSH. Configuration exclusivement via variables d'environnement (ou un
// fichier .env), voir .env.example et docs/mcp-http.md.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/bornholm/leash/internal/transport/mcphttp"
)

const reapInterval = time.Minute

func main() {
	if err := run(); err != nil {
		slog.Error("leash-mcp: fatal", "error", err)
		os.Exit(1)
	}
}

func run() error {
	configureLogging()

	cfg, err := mcphttp.LoadConfig()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	mgr := mcphttp.NewManager(cfg, mcphttp.ProductionFactory(cfg.SandboxBackend))
	mgr.StartReaper(reapInterval)
	defer mgr.Shutdown()

	srv := mcphttp.NewServer(cfg, mgr)
	defer srv.Shutdown()

	addr := envOr("LEASH_MCP_LISTEN_ADDR", "127.0.0.1:8443")
	httpServer := &http.Server{
		Addr:              addr,
		Handler:           srv,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      0, // streaming SSE : pas de timeout d'écriture global
		IdleTimeout:       120 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		slog.Info("leash-mcp: listening", "addr", addr)
		errCh <- httpServer.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("listening: %w", err)
		}
		return nil
	case <-ctx.Done():
		slog.Info("leash-mcp: shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("graceful shutdown: %w", err)
		}
		return nil
	}
}

func envOr(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}

// configureLogging installe le logger slog par défaut du process, avec un
// niveau réglable via LEASH_LOG_LEVEL ("debug", "info", "warn", "error" ;
// "info" par défaut). C'est ce logger qui reçoit à la fois les logs de
// debug par requête (mcphttp.Workspace.Exec, Manager.Acquire) et les logs
// d'audit par commande (chaque Engine de workspace écrit sur os.Stderr via
// leash.WithAuditWriter, taggé workspace_id/api_key — cf. factory.go).
func configureLogging() {
	level := slog.LevelInfo
	switch strings.ToLower(envOr("LEASH_LOG_LEVEL", "info")) {
	case "debug":
		level = slog.LevelDebug
	case "warn", "warning":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	}
	handler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})
	slog.SetDefault(slog.New(handler))
}
