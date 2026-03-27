package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"

	"github.com/bornholm/leash/internal/engine"
	mcpregistry "github.com/bornholm/leash/internal/mcp/registry"
	"github.com/bornholm/leash/internal/registry"
	"github.com/bornholm/leash/internal/security"
	mcptransport "github.com/bornholm/leash/internal/transport/mcp"
	repltransport "github.com/bornholm/leash/internal/transport/repl"
	"github.com/spf13/cobra"
)

// Vérification statique que Runner satisfait l'interface Engine.
var _ engine.Engine = (*engine.Runner)(nil)

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

var policyFile string
var auditLogFile string

var rootCmd = &cobra.Command{
	Use:   "leash",
	Short: "Sandboxed shell execution engine",
}

func init() {
	rootCmd.PersistentFlags().StringVarP(&policyFile, "policy", "p",
		envOr("LEASH_POLICY", "policies/default.yaml"), "Fichier de politique YAML")
	rootCmd.PersistentFlags().StringVarP(&auditLogFile, "audit-log", "A",
		envOr("LEASH_AUDIT_LOG", ""), "Fichier de destination de l'audit log JSON (stderr si vide)")

	rootCmd.AddCommand(replCmd, mcpCmd, execCmd)
	mcpCmd.AddCommand(mcpStdioCmd, mcpHTTPCmd)
}

// --- leash repl ---

var replCmd = &cobra.Command{
	Use:   "repl",
	Short: "Démarrer un REPL interactif",
	RunE: func(cmd *cobra.Command, args []string) error {
		eng, cleanup, err := buildEngine(cmd.Context(), policyFile, auditLogFile)
		if err != nil {
			return err
		}
		defer cleanup()
		return repltransport.New(eng, policyFile).Run()
	},
}

// --- leash mcp ---

var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "Transports MCP (stdio ou http)",
}

var mcpStdioCmd = &cobra.Command{
	Use:   "stdio",
	Short: "Démarrer le serveur MCP en mode stdio",
	RunE: func(cmd *cobra.Command, args []string) error {
		eng, cleanup, err := buildEngine(cmd.Context(), policyFile, auditLogFile)
		if err != nil {
			return err
		}
		defer cleanup()
		return mcptransport.New(eng).ServeStdio()
	},
}

var mcpHTTPAddr string

var mcpHTTPCmd = &cobra.Command{
	Use:   "http",
	Short: "Démarrer le serveur MCP en mode HTTP",
	RunE: func(cmd *cobra.Command, args []string) error {
		eng, cleanup, err := buildEngine(cmd.Context(), policyFile, auditLogFile)
		if err != nil {
			return err
		}
		defer cleanup()
		fmt.Fprintf(os.Stderr, "Starting MCP HTTP server on %s\n", mcpHTTPAddr)
		return mcptransport.New(eng).ServeHTTP(mcpHTTPAddr)
	},
}

func init() {
	mcpHTTPCmd.Flags().StringVarP(&mcpHTTPAddr, "addr", "a",
		envOr("LEASH_MCP_ADDR", ":8080"), "Adresse d'écoute du serveur HTTP")
}

// --- leash exec ---

var execScript string

var execCmd = &cobra.Command{
	Use:   "exec",
	Short: "Exécuter un script (argument ou stdin)",
	RunE: func(cmd *cobra.Command, args []string) error {
		eng, cleanup, err := buildEngine(cmd.Context(), policyFile, auditLogFile)
		if err != nil {
			return err
		}
		defer cleanup()

		script := execScript
		if script == "" {
			data, err := io.ReadAll(os.Stdin)
			if err != nil {
				return fmt.Errorf("reading stdin: %w", err)
			}
			script = string(data)
		}

		result, err := eng.ExecWithStreams(context.Background(), script, os.Stdin, os.Stdout, os.Stderr)
		if err != nil {
			return err
		}
		os.Exit(result.ExitCode)
		return nil
	},
}

func init() {
	execCmd.Flags().StringVarP(&execScript, "exec", "e", "", "Script à exécuter (lit stdin si vide)")
}

// --- helpers ---

// buildEngine construit le moteur et retourne un cleanup à appeler en fin de vie.
// Le cleanup ferme les sessions MCP et le fichier d'audit log si applicable.
func buildEngine(ctx context.Context, polFile, auditLog string) (engine.Engine, func(), error) {
	pol, err := security.LoadPolicy(polFile)
	if err != nil {
		return nil, nil, fmt.Errorf("loading policy %q: %w", polFile, err)
	}

	auditWriter, auditCloser, err := openAuditWriter(auditLog)
	if err != nil {
		return nil, nil, err
	}
	auditor := security.NewAuditLogger(slog.New(slog.NewJSONHandler(auditWriter, nil)))

	polCfg, err := security.LoadPolicyConfig(polFile)
	if err != nil {
		auditCloser()
		return nil, nil, fmt.Errorf("loading policy config: %w", err)
	}
	rl := security.NewRateLimiter(polCfg)

	reg := registry.New()

	servers, err := mcpregistry.LoadFromConfig(ctx, polCfg.MCPServers, reg)
	if err != nil {
		auditCloser()
		return nil, nil, fmt.Errorf("chargement des serveurs MCP : %w", err)
	}

	cleanup := func() {
		for _, s := range servers {
			_ = s.Session.Close()
		}
		auditCloser()
	}

	return engine.New(pol, reg, auditor, rl), cleanup, nil
}

// openAuditWriter ouvre la destination de l'audit log.
// Retourne (writer, closer, err). Si path est vide, écrit sur stderr.
func openAuditWriter(path string) (io.Writer, func(), error) {
	if path == "" {
		return os.Stderr, func() {}, nil
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o640)
	if err != nil {
		return nil, nil, fmt.Errorf("ouverture du fichier d'audit log %q : %w", path, err)
	}
	return f, func() { _ = f.Close() }, nil
}

// envOr retourne la variable d'env ou la valeur par défaut.
func envOr(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}
