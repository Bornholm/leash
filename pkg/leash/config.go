package leash

import (
	"io"
	"time"

	"github.com/bornholm/leash/internal/security"
	"github.com/bornholm/leash/pkg/skill"
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

	globalRateLimit rateSpec
	perSkillRates   map[string]rateSpec

	allowedBinaries []string
	blockedPatterns []string

	inheritEnv bool
	staticEnv  map[string]string

	enabledSkills       []string
	requireConfirmation []string

	mcpServers []security.MCPServerConfig
	skills     []*skill.Skill
	auditWriter io.Writer
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

	if len(c.perSkillRates) > 0 {
		cfg.RateLimits.PerSkill = make(map[string]security.RateSpec, len(c.perSkillRates))
		for name, spec := range c.perSkillRates {
			cfg.RateLimits.PerSkill[name] = security.RateSpec{Count: spec.count, Window: spec.window}
		}
	}

	cfg.AllowedBinaries = c.allowedBinaries
	cfg.BlockedPatterns = c.blockedPatterns
	cfg.Environment.Inherit = c.inheritEnv
	cfg.Environment.Static = c.staticEnv
	cfg.Skills.Enabled = c.enabledSkills
	cfg.Skills.RequireConfirmation = c.requireConfirmation
	cfg.MCPServers = c.mcpServers
	return cfg
}

func (c *config) applyPolicyConfig(p *security.PolicyConfig) {
	c.maxDuration = p.Execution.MaxDuration.Duration
	c.maxOutputBytes = p.Execution.MaxOutputBytes
	c.maxCommandsPerScript = p.Execution.MaxCommandsPerScript
	c.maxSubshells = p.Execution.MaxSubshells
	c.globalRateLimit = rateSpec{count: p.RateLimits.Global.Count, window: p.RateLimits.Global.Window}

	c.perSkillRates = make(map[string]rateSpec, len(p.RateLimits.PerSkill))
	for name, spec := range p.RateLimits.PerSkill {
		c.perSkillRates[name] = rateSpec{count: spec.Count, window: spec.Window}
	}

	c.allowedBinaries = p.AllowedBinaries
	c.blockedPatterns = p.BlockedPatterns
	c.inheritEnv = p.Environment.Inherit
	c.staticEnv = p.Environment.Static
	c.enabledSkills = p.Skills.Enabled
	c.requireConfirmation = p.Skills.RequireConfirmation
	c.mcpServers = p.MCPServers
}
