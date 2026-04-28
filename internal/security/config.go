package security

import (
	"fmt"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/bornholm/leash/internal/security/sandbox"
)

// Duration est un type custom pour parser les durées YAML ("30s", "1m", etc.).
type Duration struct{ time.Duration }

func (d *Duration) UnmarshalYAML(value *yaml.Node) error {
	dur, err := time.ParseDuration(value.Value)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", value.Value, err)
	}
	d.Duration = dur
	return nil
}

// RateSpec décrit une limite de débit sous la forme "N/minute" ou "N/second".
type RateSpec struct {
	Count  int
	Window time.Duration
}

func (r *RateSpec) UnmarshalYAML(value *yaml.Node) error {
	var s string
	if err := value.Decode(&s); err != nil {
		return err
	}
	var count int
	var unit string
	if _, err := fmt.Sscanf(s, "%d/%s", &count, &unit); err != nil {
		return fmt.Errorf("invalid rate spec %q, expected N/minute or N/second", s)
	}
	r.Count = count
	switch unit {
	case "second":
		r.Window = time.Second
	case "minute":
		r.Window = time.Minute
	case "hour":
		r.Window = time.Hour
	default:
		return fmt.Errorf("unknown rate unit %q, expected second/minute/hour", unit)
	}
	return nil
}

// MCPServerConfig décrit un serveur MCP externe à connecter.
type MCPServerConfig struct {
	Name      string            `yaml:"name"`
	Transport string            `yaml:"transport"` // "stdio", "http" (Streamable HTTP) ou "sse" (HTTP SSE legacy)
	Command   []string          `yaml:"command"`   // pour stdio : argv
	Env       map[string]string `yaml:"env"`       // env vars supplémentaires pour le sous-processus stdio
	URL       string            `yaml:"url"`       // pour http : endpoint SSE/Streamable
	Headers   map[string]string `yaml:"headers"`   // pour http : entêtes HTTP à injecter (ex: Authorization)
}

// PolicyConfig est la structure de configuration YAML d'une politique.
type PolicyConfig struct {
	Execution struct {
		MaxDuration          Duration `yaml:"max_duration"`
		MaxOutputBytes       int64    `yaml:"max_output_bytes"`
		MaxCommandsPerScript int      `yaml:"max_commands_per_script"`
		MaxSubshells         int      `yaml:"max_subshells"`
	} `yaml:"execution"`
	RateLimits struct {
		Global   RateSpec            `yaml:"global"`
		PerSkill map[string]RateSpec `yaml:"per_skill"`
	} `yaml:"rate_limits"`
	AllowedBinaries []string `yaml:"allowed_binaries"`
	BlockedPatterns []string `yaml:"blocked_patterns"`
	Environment struct {
		Inherit     bool              `yaml:"inherit"`
		Static      map[string]string `yaml:"static"`
		Passthrough []string          `yaml:"passthrough"`
	} `yaml:"environment"`
	Skills struct {
		Enabled             []string `yaml:"enabled"`
		RequireConfirmation []string `yaml:"require_confirmation"`
	} `yaml:"skills"`
	MCPServers []MCPServerConfig `yaml:"mcp_servers"`
	Sandbox    sandbox.Config    `yaml:"sandbox"`
}

// LoadPolicyConfig charge un fichier YAML de politique.
// Les valeurs peuvent contenir des références à des variables d'environnement
// avec la syntaxe shell : $VAR, ${VAR}, ${VAR:-default}, ${VAR-default}.
func LoadPolicyConfig(path string) (*PolicyConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading policy file: %w", err)
	}
	expanded := expandEnvVars(string(data))
	var cfg PolicyConfig
	if err := yaml.Unmarshal([]byte(expanded), &cfg); err != nil {
		return nil, fmt.Errorf("parsing policy file: %w", err)
	}
	return &cfg, nil
}

// expandEnvVars remplace les références à des variables d'environnement dans s.
// Syntaxes supportées :
//   - $VAR et ${VAR}           : substitution directe
//   - ${VAR:-default}          : valeur par défaut si VAR est absente ou vide
//   - ${VAR-default}           : valeur par défaut si VAR est absente uniquement
func expandEnvVars(s string) string {
	return os.Expand(s, func(key string) string {
		// ${VAR:-default} — défaut si absent ou vide
		if varName, defaultVal, ok := strings.Cut(key, ":-"); ok {
			if val, found := os.LookupEnv(varName); found && val != "" {
				return val
			}
			return defaultVal
		}
		// ${VAR-default} — défaut si absent seulement
		if varName, defaultVal, ok := strings.Cut(key, "-"); ok {
			if val, found := os.LookupEnv(varName); found {
				return val
			}
			return defaultVal
		}
		return os.Getenv(key)
	})
}
