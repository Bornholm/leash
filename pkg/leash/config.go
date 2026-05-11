package leash

import (
	"io"
	"time"

	"github.com/bornholm/leash/internal/security"
	"github.com/bornholm/leash/internal/security/sandbox"
	"github.com/bornholm/leash/pkg/builtin"
)

type rateSpec struct {
	count  int
	window time.Duration
}

type config struct {
	maxDuration          time.Duration
	maxOutputBytes       int64
	maxCommandsPerScript int
	maxSubshells         int

	globalRateLimit  rateSpec
	perBuiltinRates map[string]rateSpec

	allowedBinaries []string
	blockedPatterns []string

	inheritEnv bool
	staticEnv  map[string]string

	enabledBuiltins     []string
	requireConfirmation []string

	mcpServers    []security.MCPServerConfig
	builtins      []*builtin.Builtin
	auditWriter   io.Writer
	sandboxConfig sandbox.Config
}

func (c *config) toPolicyConfig() *security.PolicyConfig {
	cfg := &security.PolicyConfig{}
	cfg.Execution.MaxDuration = security.Duration{Duration: c.maxDuration}
	cfg.Execution.MaxOutputBytes = c.maxOutputBytes
	cfg.Execution.MaxCommandsPerScript = c.maxCommandsPerScript
	cfg.Execution.MaxSubshells = c.maxSubshells

	if c.globalRateLimit.count > 0 {
		cfg.RateLimits.Global = security.RateSpec{
			Count:  c.globalRateLimit.count,
			Window: c.globalRateLimit.window,
		}
	}

	if len(c.perBuiltinRates) > 0 {
		cfg.RateLimits.PerBuiltin = make(map[string]security.RateSpec, len(c.perBuiltinRates))
		for name, spec := range c.perBuiltinRates {
			cfg.RateLimits.PerBuiltin[name] = security.RateSpec{Count: spec.count, Window: spec.window}
		}
	}

	cfg.AllowedBinaries = c.allowedBinaries
	cfg.BlockedPatterns = c.blockedPatterns
	cfg.Environment.Inherit = c.inheritEnv
	cfg.Environment.Static = c.staticEnv
	cfg.Builtins.Enabled = c.enabledBuiltins
	cfg.Builtins.RequireConfirmation = c.requireConfirmation
	cfg.MCPServers = c.mcpServers
	cfg.Sandbox = c.sandboxConfig
	return cfg
}

func (c *config) applyPolicyConfig(p *security.PolicyConfig) {
	c.maxDuration = p.Execution.MaxDuration.Duration
	c.maxOutputBytes = p.Execution.MaxOutputBytes
	c.maxCommandsPerScript = p.Execution.MaxCommandsPerScript
	c.maxSubshells = p.Execution.MaxSubshells
	c.globalRateLimit = rateSpec{count: p.RateLimits.Global.Count, window: p.RateLimits.Global.Window}

	c.perBuiltinRates = make(map[string]rateSpec, len(p.RateLimits.PerBuiltin))
	for name, spec := range p.RateLimits.PerBuiltin {
		c.perBuiltinRates[name] = rateSpec{count: spec.Count, window: spec.Window}
	}

	c.allowedBinaries = p.AllowedBinaries
	c.blockedPatterns = p.BlockedPatterns
	c.inheritEnv = p.Environment.Inherit
	c.staticEnv = p.Environment.Static
	c.enabledBuiltins = p.Builtins.Enabled
	c.requireConfirmation = p.Builtins.RequireConfirmation
	c.mcpServers = p.MCPServers
	c.sandboxConfig = p.Sandbox
}
