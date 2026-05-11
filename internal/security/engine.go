package security

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"mvdan.cc/sh/v3/syntax"

	"github.com/bornholm/leash/internal/security/sandbox"
)

type policyEngine struct {
	cfg             *PolicyConfig
	allowedBinaries  map[string]struct{}
	enabledBuiltins  map[string]struct{}
	confirmBuiltins  map[string]struct{}
}

func NewPolicyEngine(cfg *PolicyConfig) PolicyEngine {
	allowed := make(map[string]struct{}, len(cfg.AllowedBinaries))
	for _, b := range cfg.AllowedBinaries {
		allowed[b] = struct{}{}
	}
	enabled := make(map[string]struct{}, len(cfg.Builtins.Enabled))
	for _, s := range cfg.Builtins.Enabled {
		enabled[s] = struct{}{}
	}
	confirm := make(map[string]struct{}, len(cfg.Builtins.RequireConfirmation))
	for _, s := range cfg.Builtins.RequireConfirmation {
		confirm[s] = struct{}{}
	}
	return &policyEngine{
		cfg:              cfg,
		allowedBinaries:  allowed,
		enabledBuiltins:  enabled,
		confirmBuiltins:  confirm,
	}
}

// LoadPolicy charge et crée un PolicyEngine depuis un fichier YAML.
func LoadPolicy(path string) (PolicyEngine, error) {
	cfg, err := LoadPolicyConfig(path)
	if err != nil {
		return nil, err
	}
	return NewPolicyEngine(cfg), nil
}

func (p *policyEngine) ValidateAST(prog any) error {
	file, ok := prog.(*syntax.File)
	if !ok {
		return fmt.Errorf("ValidateAST: expected *syntax.File, got %T", prog)
	}

	var cmdCount, subshellCount int
	var validationErr error

	syntax.Walk(file, func(node syntax.Node) bool {
		if validationErr != nil {
			return false
		}
		switch n := node.(type) {
		case *syntax.CallExpr:
			cmdCount++
			if p.cfg.Execution.MaxCommandsPerScript > 0 && cmdCount > p.cfg.Execution.MaxCommandsPerScript {
				validationErr = fmt.Errorf("script exceeds maximum command count (%d)", p.cfg.Execution.MaxCommandsPerScript)
				return false
			}
		case *syntax.Subshell:
			subshellCount++
			if p.cfg.Execution.MaxSubshells > 0 && subshellCount > p.cfg.Execution.MaxSubshells {
				validationErr = fmt.Errorf("script exceeds maximum subshell depth (%d)", p.cfg.Execution.MaxSubshells)
				return false
			}
			_ = n
		case *syntax.Stmt:
			if n.Background {
				validationErr = fmt.Errorf("background jobs (&) are not allowed")
				return false
			}
		}
		return true
	})

	return validationErr
}

func (p *policyEngine) CanExecuteBuiltin(ctx context.Context, name string, args []string) error {
	if len(p.enabledBuiltins) > 0 {
		if _, ok := p.enabledBuiltins[name]; !ok {
			return fmt.Errorf("builtin %q is not enabled by policy", name)
		}
	}

	if _, ok := p.confirmBuiltins[name]; ok {
		envKey := "CONFIRM_" + strings.ToUpper(strings.ReplaceAll(name, "-", "_"))
		if os.Getenv(envKey) != "yes" {
			return fmt.Errorf("builtin %q requires confirmation: set %s=yes", name, envKey)
		}
	}

	return nil
}

func (p *policyEngine) IsAllowedBinary(name string) bool {
	_, ok := p.allowedBinaries[name]
	return ok
}

func (p *policyEngine) MaxExecDuration() time.Duration {
	if p.cfg.Execution.MaxDuration.Duration == 0 {
		return 30 * time.Second
	}
	return p.cfg.Execution.MaxDuration.Duration
}

func (p *policyEngine) MaxOutputBytes() int64 {
	if p.cfg.Execution.MaxOutputBytes == 0 {
		return 1024 * 1024 // 1 MB par défaut
	}
	return p.cfg.Execution.MaxOutputBytes
}

func (p *policyEngine) SafeEnvironment() map[string]string {
	env := make(map[string]string, len(p.cfg.Environment.Static))
	for k, v := range p.cfg.Environment.Static {
		env[k] = v
	}
	for _, key := range p.cfg.Environment.Passthrough {
		if val, ok := os.LookupEnv(key); ok {
			env[key] = val
		}
	}
	return env
}

func (p *policyEngine) IsBlockedPattern(script string) (bool, string) {
	for _, pattern := range p.cfg.BlockedPatterns {
		if strings.Contains(script, pattern) {
			return true, pattern
		}
	}
	return false, ""
}

func (p *policyEngine) EnabledBuiltins() []string {
	return p.cfg.Builtins.Enabled
}

func (p *policyEngine) AllowedBinaries() []string {
	return p.cfg.AllowedBinaries
}

func (p *policyEngine) SandboxConfig() sandbox.Config {
	return p.cfg.Sandbox
}
