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

	EnabledBuiltins() []string

	AllowedBinaries() []string

	SandboxConfig() sandbox.Config
}
