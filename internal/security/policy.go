package security

import (
	"context"
	"time"

	"github.com/bornholm/leash/internal/security/sandbox"
)

// PolicyEngine définit les règles de sécurité appliquées par le moteur d'exécution.
// L'interface utilise `any` pour le paramètre AST afin d'éviter de tirer mvdan.cc/sh/v3
// comme dépendance de ce package. L'implémentation concrète fera le type assert.
type PolicyEngine interface {
	// ValidateAST analyse statiquement le script parsé avant exécution.
	// prog est de type *syntax.File (mvdan.cc/sh/v3/syntax).
	ValidateAST(prog any) error

	// CanExecuteSkill vérifie si le skill peut être exécuté dans ce contexte.
	// Retourne une erreur descriptive si refusé (désactivé, confirmation requise, etc.).
	CanExecuteSkill(ctx context.Context, name string, args []string) error

	// IsAllowedBinary retourne vrai si le binaire système est dans l'allowlist.
	IsAllowedBinary(name string) bool

	// MaxExecDuration retourne la durée maximale d'un script.
	MaxExecDuration() time.Duration

	// MaxOutputBytes retourne la taille maximale de stdout+stderr combinés.
	MaxOutputBytes() int64

	// SafeEnvironment retourne les variables d'environnement à injecter dans le script.
	// Ne retourne jamais les variables de l'environnement hôte.
	SafeEnvironment() map[string]string

	// IsBlockedPattern vérifie si le script contient un pattern bloqué (analyse textuelle).
	// Retourne (true, pattern) si bloqué, (false, "") sinon.
	IsBlockedPattern(script string) (bool, string)

	// EnabledSkills retourne la liste des skills activés.
	EnabledSkills() []string

	// AllowedBinaries retourne la liste des binaires système autorisés par la politique.
	AllowedBinaries() []string

	// SandboxConfig retourne la configuration sandbox de la politique.
	SandboxConfig() sandbox.Config
}
