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
	"github.com/bornholm/leash/internal/security/sandbox"
	mcptransport "github.com/bornholm/leash/internal/transport/mcp"
	repltransport "github.com/bornholm/leash/internal/transport/repl"
	"github.com/bornholm/leash/pkg/builtin"
	builtinShell "github.com/bornholm/leash/pkg/builtin/shell"
	builtinTengo "github.com/bornholm/leash/pkg/builtin/tengo"
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
var tengoBuiltinDirs []string
var shellBuiltinDirs []string

var rootCmd = &cobra.Command{
	Use:   "leash",
	Short: "Sandboxed shell execution engine",
}

func init() {
	rootCmd.PersistentFlags().StringVarP(&policyFile, "policy", "p",
		envOr("LEASH_POLICY", "policies/default.yaml"), "Fichier de politique YAML")
	rootCmd.PersistentFlags().StringVarP(&auditLogFile, "audit-log", "A",
		envOr("LEASH_AUDIT_LOG", ""), "Fichier de destination de l'audit log JSON (stderr si vide)")
	rootCmd.PersistentFlags().StringArrayVar(&tengoBuiltinDirs, "tengo-builtins", nil, "Répertoire(s) de builtins Tengo (*.tengo)")
	rootCmd.PersistentFlags().StringArrayVar(&shellBuiltinDirs, "shell-builtins", nil, "Répertoire(s) de builtins shell (*.sh)")

	rootCmd.AddCommand(replCmd, mcpCmd, execCmd)
	mcpCmd.AddCommand(mcpStdioCmd)
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
	Short: "Transports MCP",
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

// --- leash exec ---

var execScript string

var execCmd = &cobra.Command{
	Use:   "exec [script-file]",
	Short: "Exécuter un script (fichier, --exec ou stdin)",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		eng, cleanup, err := buildEngine(cmd.Context(), policyFile, auditLogFile)
		if err != nil {
			return err
		}
		defer cleanup()

		var script string
		switch {
		case execScript != "":
			script = execScript
		case len(args) == 1:
			data, err := os.ReadFile(args[0])
			if err != nil {
				return fmt.Errorf("reading script file %q: %w", args[0], err)
			}
			script = string(data)
		default:
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

	for _, dir := range tengoBuiltinDirs {
		builtins, err := builtinTengo.LoadDir(dir)
		if err != nil {
			auditCloser()
			return nil, nil, fmt.Errorf("chargement des builtins Tengo depuis %q : %w", dir, err)
		}
		for _, sk := range builtins {
			if err := reg.Register(sk); err != nil {
				auditCloser()
				return nil, nil, fmt.Errorf("enregistrement du builtin Tengo %q : %w", sk.Name, err)
			}
		}
	}

	for _, dir := range shellBuiltinDirs {
		builtins, err := builtinShell.LoadDir(dir)
		if err != nil {
			auditCloser()
			return nil, nil, fmt.Errorf("chargement des builtins shell depuis %q : %w", dir, err)
		}
		for _, sk := range builtins {
			if err := reg.Register(sk); err != nil {
				auditCloser()
				return nil, nil, fmt.Errorf("enregistrement du builtin shell %q : %w", sk.Name, err)
			}
		}
	}

	sb, err := sandbox.New(polCfg.Sandbox)
	if err != nil {
		auditCloser()
		return nil, nil, fmt.Errorf("construction du sandbox : %w", err)
	}

	servers, err := mcpregistry.LoadFromConfig(ctx, polCfg.MCPServers, reg)
	if err != nil {
		_ = sb.Close()
		auditCloser()
		return nil, nil, fmt.Errorf("chargement des serveurs MCP : %w", err)
	}

	cleanup := func() {
		for _, s := range servers {
			_ = s.Session.Close()
		}
		_ = sb.Close()
		auditCloser()
	}

	eng := engine.New(pol, reg, auditor, rl, sb)

	helpBuiltin := builtin.New("leash-help").
		Description("List all available shell commands with their usage and flags.").
		Example("List all commands", "leash-help").
		Handle(func(ctx context.Context, c *builtin.Call) error {
			fmt.Fprint(c.Stdout, reg.GenerateManifest())
			return nil
		})

	if err := reg.Register(helpBuiltin); err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("enregistrement du builtin help : %w", err)
	}

	return eng, cleanup, nil
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
