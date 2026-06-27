package security

import (
	"context"
	"time"

	"github.com/bornholm/leash/internal/security/sandbox"
)

type PolicyEngine interface {
	ValidateAST(prog any) error

	CanExecuteBuiltin(ctx context.Context, name string, args []string) error

	IsAllowedBinary(name string) bool

	MaxExecDuration() time.Duration

	MaxOutputBytes() int64

	SafeEnvironment() map[string]string

	IsBlockedPattern(script string) (bool, string)

	// EnabledBuiltins retourne la whitelist configurée. Ambigu seul : nil
	// signifie aussi bien "builtins désactivés" que "whitelist vide = tous
	// les builtins enregistrés sont autorisés" (cf. BuiltinsDisabled pour
	// lever l'ambiguïté).
	EnabledBuiltins() []string

	// BuiltinsDisabled indique si les builtins sont entièrement désactivés
	// (cfg.Builtins.Disabled), indépendamment du contenu de la whitelist.
	BuiltinsDisabled() bool

	AllowedBinaries() []string

	SandboxConfig() sandbox.Config
}
