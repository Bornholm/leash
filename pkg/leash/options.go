package leash

import (
	"fmt"
	"io"
	"time"

	"github.com/bornholm/leash/internal/security"
	"github.com/bornholm/leash/internal/security/sandbox"
	"github.com/bornholm/leash/pkg/skill"
	skillshell "github.com/bornholm/leash/pkg/skill/shell"
	skilltengo "github.com/bornholm/leash/pkg/skill/tengo"
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

// WithGlobalRateLimit définit la limite de débit globale (toutes commandes confondues).
func WithGlobalRateLimit(count int, window time.Duration) Option {
	return func(c *config) error {
		c.globalRateLimit = rateSpec{count: count, window: window}
		return nil
	}
}

// WithSkillRateLimit définit la limite de débit pour un skill spécifique.
func WithSkillRateLimit(name string, count int, window time.Duration) Option {
	return func(c *config) error {
		if c.perSkillRates == nil {
			c.perSkillRates = make(map[string]rateSpec)
		}
		c.perSkillRates[name] = rateSpec{count: count, window: window}
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

// WithEnabledSkills définit la liste blanche des skills autorisés (vide = tous autorisés).
func WithEnabledSkills(skills ...string) Option {
	return func(c *config) error {
		c.enabledSkills = skills
		return nil
	}
}

// WithRequireConfirmation définit les skills nécessitant une confirmation explicite
// (variable d'env CONFIRM_<SKILL>=yes).
func WithRequireConfirmation(skills ...string) Option {
	return func(c *config) error {
		c.requireConfirmation = skills
		return nil
	}
}

// WithSkill enregistre un skill dans l'Engine au moment de la construction.
func WithSkill(s *skill.Skill) Option {
	return func(c *config) error {
		if s == nil {
			return fmt.Errorf("leash: WithSkill: skill cannot be nil")
		}
		c.skills = append(c.skills, s)
		return nil
	}
}

// WithSkills enregistre plusieurs skills dans l'Engine.
func WithSkills(skills ...*skill.Skill) Option {
	return func(c *config) error {
		for _, s := range skills {
			if s == nil {
				return fmt.Errorf("leash: WithSkills: skill cannot be nil")
			}
			c.skills = append(c.skills, s)
		}
		return nil
	}
}

// WithTengoSkill compile un script Tengo et enregistre le skill résultant dans l'Engine.
// src est le contenu complet du fichier (frontmatter YAML + corps du script).
func WithTengoSkill(src []byte) Option {
	return func(c *config) error {
		sk, err := skilltengo.LoadScript(src)
		if err != nil {
			return fmt.Errorf("leash: WithTengoSkill: %w", err)
		}
		c.skills = append(c.skills, sk)
		return nil
	}
}

// WithTengoSkillDir charge tous les fichiers *.tengo d'un répertoire comme skills Tengo.
// Les erreurs de compilation de chaque fichier sont agrégées et retournées.
func WithTengoSkillDir(dir string) Option {
	return func(c *config) error {
		skills, err := skilltengo.LoadDir(dir)
		if err != nil {
			return fmt.Errorf("leash: WithTengoSkillDir: %w", err)
		}
		c.skills = append(c.skills, skills...)
		return nil
	}
}

// WithShellSkill charge un script shell et enregistre le skill résultant dans l'Engine.
// src est le contenu complet du fichier (frontmatter heredoc + corps du script).
func WithShellSkill(src []byte) Option {
	return func(c *config) error {
		sk, err := skillshell.LoadScript(src)
		if err != nil {
			return fmt.Errorf("leash: WithShellSkill: %w", err)
		}
		c.skills = append(c.skills, sk)
		return nil
	}
}

// WithShellSkillDir charge tous les fichiers *.sh d'un répertoire comme skills shell.
// Les erreurs de chaque fichier sont agrégées et retournées.
func WithShellSkillDir(dir string) Option {
	return func(c *config) error {
		skills, err := skillshell.LoadDir(dir)
		if err != nil {
			return fmt.Errorf("leash: WithShellSkillDir: %w", err)
		}
		c.skills = append(c.skills, skills...)
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
