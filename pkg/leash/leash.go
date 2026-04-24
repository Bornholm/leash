// Package leash expose une API publique pour créer et configurer un moteur d'exécution
// shell sandboxé. Utiliser New() avec des options pour construire un Engine.
//
// Exemple :
//
//	eng, cleanup, err := leash.New(ctx,
//	    leash.WithMaxDuration(60 * time.Second),
//	    leash.WithAllowedBinaries("grep", "awk"),
//	    leash.WithEnvVar("HOME", "/tmp/sandbox"),
//	    leash.WithAuditWriter(os.Stderr),
//	)
//	if err != nil { ... }
//	defer cleanup()
//
//	result, err := eng.Exec(ctx, "grep foo /var/log/app.log | head -20")
package leash

import (
	"context"
	"fmt"
	"io"
	"log/slog"

	"github.com/bornholm/leash/internal/engine"
	mcpregistry "github.com/bornholm/leash/internal/mcp/registry"
	"github.com/bornholm/leash/internal/registry"
	"github.com/bornholm/leash/internal/security"
	"github.com/bornholm/leash/internal/security/sandbox"
)

// Engine est l'interface publique du moteur d'exécution shell.
// Les méthodes Registry() et Policy() de l'interface interne ne sont pas exposées
// car elles retournent des types internes.
type Engine interface {
	// Exec exécute un script et capture stdout/stderr dans le résultat.
	Exec(ctx context.Context, script string) (*ExecResult, error)

	// ExecWithStreams exécute un script avec des streams I/O fournis par l'appelant.
	ExecWithStreams(ctx context.Context, script string, stdin io.Reader, stdout, stderr io.Writer) (*ExecResult, error)
}

// Vérification statique que *engine.Runner satisfait l'interface Engine publique.
var _ Engine = (*engine.Runner)(nil)

// Type aliases permettant au code externe de nommer ces types sans importer internal/.

// ExecResult contient le résultat d'une exécution shell.
type ExecResult = engine.ExecResult

// AuditTrail contient l'historique des commandes exécutées pendant un script.
type AuditTrail = security.AuditTrail

// CommandRecord décrit l'exécution d'une commande individuelle.
type CommandRecord = security.CommandRecord

// MCPServerConfig décrit un serveur MCP externe à connecter.
type MCPServerConfig = security.MCPServerConfig

// New crée un Engine configuré selon les options fournies.
// La fonction retourne également un cleanup à appeler en fin de vie (fermeture des sessions MCP).
// Les options sont appliquées dans l'ordre : WithPolicyFile doit être en premier si utilisé.
func New(ctx context.Context, opts ...Option) (Engine, func(), error) {
	cfg := defaultConfig()

	for _, opt := range opts {
		if err := opt(cfg); err != nil {
			return nil, nil, fmt.Errorf("leash: applying option: %w", err)
		}
	}

	polCfg := cfg.toPolicyConfig()
	pol := security.NewPolicyEngine(polCfg)
	rl := security.NewRateLimiter(polCfg)

	sb, err := sandbox.New(cfg.sandboxConfig)
	if err != nil {
		return nil, nil, fmt.Errorf("leash: building sandbox: %w", err)
	}

	var auditor *security.AuditLogger
	if cfg.auditWriter != nil {
		handler := slog.NewJSONHandler(cfg.auditWriter, nil)
		auditor = security.NewAuditLogger(slog.New(handler))
	}

	reg := registry.New()
	for _, sk := range cfg.skills {
		if err := reg.Register(sk); err != nil {
			return nil, nil, fmt.Errorf("leash: registering skill %q: %w", sk.Name, err)
		}
	}

	var cleanupFns []func()
	cleanupFns = append(cleanupFns, func() { _ = sb.Close() })

	if len(cfg.mcpServers) > 0 {
		servers, err := mcpregistry.LoadFromConfig(ctx, cfg.mcpServers, reg)
		if err != nil {
			return nil, nil, fmt.Errorf("leash: loading MCP servers: %w", err)
		}
		cleanupFns = append(cleanupFns, func() {
			for _, s := range servers {
				_ = s.Session.Close()
			}
		})
	}

	eng := engine.New(pol, reg, auditor, rl, sb)

	cleanup := func() {
		for _, fn := range cleanupFns {
			fn()
		}
	}

	return eng, cleanup, nil
}
