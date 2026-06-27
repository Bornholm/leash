package leash

import (
	"fmt"
	"io"
	"time"

	"github.com/bornholm/leash/internal/security"
	"github.com/bornholm/leash/internal/security/sandbox"
	"github.com/bornholm/leash/pkg/builtin"
	builtinShell "github.com/bornholm/leash/pkg/builtin/shell"
	builtinTengo "github.com/bornholm/leash/pkg/builtin/tengo"
)

// Option est une fonction de configuration pour New().
type Option func(*config) error

// WithMaxDuration définit la durée maximale d'exécution d'un script.
func WithMaxDuration(d time.Duration) Option {
	return func(c *config) error {
		c.maxDuration = d
		return nil
	}
}

// WithMaxOutputBytes définit la taille maximale de la sortie combinée (stdout+stderr).
func WithMaxOutputBytes(n int64) Option {
	return func(c *config) error {
		c.maxOutputBytes = n
		return nil
	}
}

// WithMaxCommandsPerScript définit le nombre maximum de commandes par script.
func WithMaxCommandsPerScript(n int) Option {
	return func(c *config) error {
		c.maxCommandsPerScript = n
		return nil
	}
}

// WithMaxSubshells définit le nombre maximum de sous-shells autorisés.
func WithMaxSubshells(n int) Option {
	return func(c *config) error {
		c.maxSubshells = n
		return nil
	}
}

func WithGlobalRateLimit(count int, window time.Duration) Option {
	return func(c *config) error {
		c.globalRateLimit = rateSpec{count: count, window: window}
		return nil
	}
}

func WithBuiltinRateLimit(name string, count int, window time.Duration) Option {
	return func(c *config) error {
		if c.perBuiltinRates == nil {
			c.perBuiltinRates = make(map[string]rateSpec)
		}
		c.perBuiltinRates[name] = rateSpec{count: count, window: window}
		return nil
	}
}

// WithAllowedBinaries remplace la liste des binaires système autorisés.
func WithAllowedBinaries(binaries ...string) Option {
	return func(c *config) error {
		c.allowedBinaries = binaries
		return nil
	}
}

// WithBlockedPatterns remplace la liste des patterns bloqués (vérifiés avant le parsing).
func WithBlockedPatterns(patterns ...string) Option {
	return func(c *config) error {
		c.blockedPatterns = patterns
		return nil
	}
}

// WithStaticEnv remplace l'environnement statique injecté dans chaque exécution.
func WithStaticEnv(env map[string]string) Option {
	return func(c *config) error {
		c.staticEnv = env
		return nil
	}
}

// WithEnvVar ajoute ou remplace une variable d'environnement individuelle.
func WithEnvVar(key, value string) Option {
	return func(c *config) error {
		if c.staticEnv == nil {
			c.staticEnv = make(map[string]string)
		}
		c.staticEnv[key] = value
		return nil
	}
}

func WithEnabledBuiltins(builtins ...string) Option {
	return func(c *config) error {
		c.enabledBuiltins = builtins
		return nil
	}
}

// WithBuiltinsDisabled désactive entièrement tous les builtins, sans ambiguïté.
// Contrairement à WithEnabledBuiltins(), une liste vide via cette dernière
// signifie « tous autorisés » ; WithBuiltinsDisabled() signifie toujours
// « aucun builtin », et prend le pas sur WithEnabledBuiltins() quel que soit
// l'ordre d'application des options.
func WithBuiltinsDisabled() Option {
	return func(c *config) error {
		c.builtinsDisabled = true
		return nil
	}
}

func WithRequireConfirmation(builtins ...string) Option {
	return func(c *config) error {
		c.requireConfirmation = builtins
		return nil
	}
}

func WithBuiltin(b *builtin.Builtin) Option {
	return func(c *config) error {
		if b == nil {
			return fmt.Errorf("leash: WithBuiltin: builtin cannot be nil")
		}
		c.builtins = append(c.builtins, b)
		return nil
	}
}

func WithBuiltins(builtins ...*builtin.Builtin) Option {
	return func(c *config) error {
		for _, b := range builtins {
			if b == nil {
				return fmt.Errorf("leash: WithBuiltins: builtin cannot be nil")
			}
			c.builtins = append(c.builtins, b)
		}
		return nil
	}
}

func WithTengoBuiltin(src []byte) Option {
	return func(c *config) error {
		sk, err := builtinTengo.LoadScript(src)
		if err != nil {
			return fmt.Errorf("leash: WithTengoBuiltin: %w", err)
		}
		c.builtins = append(c.builtins, sk)
		return nil
	}
}

func WithTengoBuiltinDir(dir string) Option {
	return func(c *config) error {
		builtins, err := builtinTengo.LoadDir(dir)
		if err != nil {
			return fmt.Errorf("leash: WithTengoBuiltinDir: %w", err)
		}
		c.builtins = append(c.builtins, builtins...)
		return nil
	}
}

func WithShellBuiltin(src []byte) Option {
	return func(c *config) error {
		sk, err := builtinShell.LoadScript(src)
		if err != nil {
			return fmt.Errorf("leash: WithShellBuiltin: %w", err)
		}
		c.builtins = append(c.builtins, sk)
		return nil
	}
}

func WithShellBuiltinDir(dir string) Option {
	return func(c *config) error {
		builtins, err := builtinShell.LoadDir(dir)
		if err != nil {
			return fmt.Errorf("leash: WithShellBuiltinDir: %w", err)
		}
		c.builtins = append(c.builtins, builtins...)
		return nil
	}
}

// WithMCPServer connecte un serveur MCP externe et enregistre ses tools comme skills.
func WithMCPServer(cfg security.MCPServerConfig) Option {
	return func(c *config) error {
		c.mcpServers = append(c.mcpServers, cfg)
		return nil
	}
}

// WithAuditWriter définit la destination des logs d'audit JSON.
// Passer io.Discard pour désactiver sans supprimer l'audit trail.
func WithAuditWriter(w io.Writer) Option {
	return func(c *config) error {
		c.auditWriter = w
		return nil
	}
}

// WithAuditAttrs ajoute des paires clé/valeur statiques (format slog : "k1",
// v1, "k2", v2, ...) à chaque ligne de log d'audit produite par cet Engine.
// Permet à un appelant qui gère plusieurs Engine (ex. un workspace par
// tenant) de distinguer les commandes exécutées par chacun dans des logs
// partagés, par exemple : WithAuditAttrs("workspace_id", id, "api_key", name).
// Sans effet si WithAuditWriter n'est pas également utilisé.
func WithAuditAttrs(args ...any) Option {
	return func(c *config) error {
		c.auditAttrs = append(c.auditAttrs, args...)
		return nil
	}
}

// WithPersistentTmp active le partage de /tmp entre toutes les commandes d'un même script.
// Quand activé, un répertoire temporaire hôte est créé pour chaque appel ExecWithStreams et
// monté comme /tmp dans chaque processus bwrap, ce qui permet aux fichiers écrits dans /tmp
// de persister entre les commandes du script. Requiert backend bwrap avec /tmp dans tmpfs.
func WithPersistentTmp(enabled bool) Option {
	return func(c *config) error {
		c.sandboxConfig.PersistentTmp = enabled
		return nil
	}
}

// WithSandbox active l'isolation filesystem avec la configuration fournie.
// Si backend est "none" ou vide, aucun changement comportemental.
// WithWorkDir définit le répertoire de travail du shell (affecte $PWD, les
// redirections I/O et les chemins relatifs dans les builtins mvdan).
// Doit correspondre au répertoire hôte réel du workspace quand un sandbox
// est configuré, afin que le shell voie le même répertoire que bwrap --bind.
func WithWorkDir(dir string) Option {
	return func(c *config) error {
		c.workDir = dir
		return nil
	}
}

func WithSandbox(cfg sandbox.Config) Option {
	return func(c *config) error {
		c.sandboxConfig = cfg
		return nil
	}
}

// WithPolicyFile charge une politique depuis un fichier YAML et remplace la configuration par défaut.
// Les options appliquées après WithPolicyFile surchargent les valeurs du fichier.
// WithPolicyFile doit être placé en premier dans la liste des options.
func WithPolicyFile(path string) Option {
	return func(c *config) error {
		polCfg, err := security.LoadPolicyConfig(path)
		if err != nil {
			return fmt.Errorf("leash: WithPolicyFile: %w", err)
		}
		c.applyPolicyConfig(polCfg)
		return nil
	}
}
