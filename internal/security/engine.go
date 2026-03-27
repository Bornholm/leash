package security

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"mvdan.cc/sh/v3/syntax"
)

// policyEngine est l'implémentation concrète de PolicyEngine.
type policyEngine struct {
	cfg             *PolicyConfig
	allowedBinaries map[string]struct{}
	enabledSkills   map[string]struct{}
	confirmSkills   map[string]struct{}
}

// NewPolicyEngine crée un PolicyEngine à partir d'une configuration.
func NewPolicyEngine(cfg *PolicyConfig) PolicyEngine {
	allowed := make(map[string]struct{}, len(cfg.AllowedBinaries))
	for _, b := range cfg.AllowedBinaries {
		allowed[b] = struct{}{}
	}
	enabled := make(map[string]struct{}, len(cfg.Skills.Enabled))
	for _, s := range cfg.Skills.Enabled {
		enabled[s] = struct{}{}
	}
	confirm := make(map[string]struct{}, len(cfg.Skills.RequireConfirmation))
	for _, s := range cfg.Skills.RequireConfirmation {
		confirm[s] = struct{}{}
	}
	return &policyEngine{
		cfg:             cfg,
		allowedBinaries: allowed,
		enabledSkills:   enabled,
		confirmSkills:   confirm,
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

func (p *policyEngine) CanExecuteSkill(ctx context.Context, name string, args []string) error {
	// Vérifier si le skill est activé (liste vide = tous activés)
	if len(p.enabledSkills) > 0 {
		if _, ok := p.enabledSkills[name]; !ok {
			return fmt.Errorf("skill %q is not enabled by policy", name)
		}
	}

	// Vérifier si une confirmation est requise
	if _, ok := p.confirmSkills[name]; ok {
		envKey := "CONFIRM_" + strings.ToUpper(strings.ReplaceAll(name, "-", "_"))
		if os.Getenv(envKey) != "yes" {
			return fmt.Errorf("skill %q requires confirmation: set %s=yes", name, envKey)
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

func (p *policyEngine) EnabledSkills() []string {
	return p.cfg.Skills.Enabled
}

func (p *policyEngine) AllowedBinaries() []string {
	return p.cfg.AllowedBinaries
}
