package mcphttp

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/time/rate"

	"github.com/joho/godotenv"

	"github.com/bornholm/leash/internal/security"
)

// ServerConfig est la configuration complète du serveur MCP HTTP.
type ServerConfig struct {
	hmacSecret []byte

	WorkspaceRoot string
	APIKeys       []*APIKeyConfig
	TTL           time.Duration

	// DiscHeader est le nom du header HTTP portant le discriminant (ex. "X-Workspace").
	DiscHeader string
	// DiscURLParam est le nom de la variable d'URL portant le discriminant (ex. "workspace").
	DiscURLParam string

	// MaxWorkspaces est le nombre maximal de workspaces actifs simultanément. 0 = illimité.
	MaxWorkspaces int

	// MaxRequestBodyBytes est la taille maximale du corps des requêtes HTTP. 0 = défaut.
	MaxRequestBodyBytes int64

	// HTTPRateLimit et HTTPBurst configurent le rate-limiting par IP.
	// HTTPRateLimit == 0 désactive le rate-limiting.
	HTTPRateLimit rate.Limit
	HTTPBurst     int

	// TrustProxyHeaders permet d'extraire l'IP réelle depuis X-Forwarded-For / X-Real-IP.
	// N'activer que si un reverse proxy de confiance contrôle ces headers.
	TrustProxyHeaders bool
}

// APIKeyConfig décrit une clé API et le contexte qui lui est associé.
// La clé brute n'est jamais conservée : seul son empreinte SHA-256 persiste.
type APIKeyConfig struct {
	Name string

	keyHash [32]byte

	// WorkspaceID, si non vide, surcharge tout discriminant dérivé du
	// header/URL pour cette clé : toutes les requêtes authentifiées avec
	// cette clé partagent le même workspace.
	WorkspaceID string

	// Env contient des variables d'environnement supplémentaires injectées
	// dans le workspace pour cette clé.
	Env map[string]string

	// PolicyFile, si non vide, pointe vers un fichier de policy YAML
	// (security.PolicyConfig) propre à cette clé : binaires autorisés,
	// builtins, serveurs MCP, et éventuellement son propre sandbox. Le
	// répertoire de travail du workspace est toujours injecté dans le
	// sandbox résultant, quoi que dise le fichier (cf. ProductionFactory).
	// Si vide, la clé utilise le sandbox durci par défaut avec tous les
	// builtins désactivés (C2/C4).
	PolicyFile string
}


const (
	envHMACSecret       = "LEASH_HMAC_SECRET"
	envWorkspaceRoot    = "LEASH_WORKSPACE_ROOT"
	envTTL              = "LEASH_TTL"
	envDiscHeader       = "LEASH_DISC_HEADER"
	envDiscURLParam     = "LEASH_DISC_URL_PARAM"
	envMaxWorkspaces    = "LEASH_MAX_WORKSPACES"
	envMaxRequestBody   = "LEASH_MAX_REQUEST_BODY_BYTES"
	envHTTPRateLimit    = "LEASH_HTTP_RATE_LIMIT"
	envHTTPBurst        = "LEASH_HTTP_BURST"
	envTrustProxy       = "LEASH_TRUST_PROXY_HEADERS"

	defaultWorkspaceRoot       = "./leash-workspaces"
	defaultTTL                 = 30 * time.Minute
	defaultDiscHeader          = "X-Workspace"
	defaultDiscURLParam        = "workspace"
	defaultMaxWorkspaces       = 100
	defaultMaxRequestBodyBytes = int64(1 << 20) // 1 MiB
	defaultHTTPRateLimitCount  = 60
	defaultHTTPBurst           = 20

	minHMACSecretLen = 32
	minAPIKeyLen     = 20

	apiKeyPrefix = "LEASH_APIKEY_"
)

var apiKeyNamePattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// LoadConfig construit la configuration serveur depuis l'environnement
// process, après avoir chargé un éventuel fichier .env (sans écraser des
// variables déjà présentes dans l'environnement). Échoue immédiatement si
// LEASH_HMAC_SECRET est absent ou trop court (fail-fast).
func LoadConfig() (*ServerConfig, error) {
	_ = godotenv.Load() // best-effort ; l'environnement réel a priorité

	secret := os.Getenv(envHMACSecret)
	if secret == "" {
		return nil, fmt.Errorf("%s is required", envHMACSecret)
	}
	if len(secret) < minHMACSecretLen {
		return nil, fmt.Errorf("%s must be at least %d characters (got %d) — use a cryptographically random value",
			envHMACSecret, minHMACSecretLen, len(secret))
	}

	cfg := &ServerConfig{
		hmacSecret:          []byte(secret),
		WorkspaceRoot:       getEnvDefault(envWorkspaceRoot, defaultWorkspaceRoot),
		DiscHeader:          getEnvDefault(envDiscHeader, defaultDiscHeader),
		DiscURLParam:        getEnvDefault(envDiscURLParam, defaultDiscURLParam),
		TTL:                 defaultTTL,
		MaxWorkspaces:       defaultMaxWorkspaces,
		MaxRequestBodyBytes: defaultMaxRequestBodyBytes,
		HTTPBurst:           defaultHTTPBurst,
		TrustProxyHeaders:   os.Getenv(envTrustProxy) == "true",
	}

	if raw := os.Getenv(envTTL); raw != "" {
		d, err := time.ParseDuration(raw)
		if err != nil {
			return nil, fmt.Errorf("%s: invalid duration %q: %w", envTTL, raw, err)
		}
		cfg.TTL = d
	}

	if raw := os.Getenv(envMaxWorkspaces); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 {
			return nil, fmt.Errorf("%s: invalid value %q, expected non-negative integer", envMaxWorkspaces, raw)
		}
		cfg.MaxWorkspaces = n
	}

	if raw := os.Getenv(envMaxRequestBody); raw != "" {
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || n <= 0 {
			return nil, fmt.Errorf("%s: invalid value %q, expected positive integer (bytes)", envMaxRequestBody, raw)
		}
		cfg.MaxRequestBodyBytes = n
	}

	// Rate-limiting HTTP : "N/minute" ou "N/second" ou "N/hour", identique à RateSpec.
	// Par défaut : 60/minute avec burst 20.
	rateLimitStr := getEnvDefault(envHTTPRateLimit, fmt.Sprintf("%d/minute", defaultHTTPRateLimitCount))
	rl, err := parseRateLimit(rateLimitStr)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", envHTTPRateLimit, err)
	}
	cfg.HTTPRateLimit = rl

	if raw := os.Getenv(envHTTPBurst); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 {
			return nil, fmt.Errorf("%s: invalid value %q, expected positive integer", envHTTPBurst, raw)
		}
		cfg.HTTPBurst = n
	}

	keys, err := loadAPIKeys(os.Environ())
	if err != nil {
		return nil, err
	}
	if len(keys) == 0 {
		return nil, errors.New("no API keys configured (expected at least one LEASH_APIKEY_<NAME> variable)")
	}
	cfg.APIKeys = keys

	return cfg, nil
}

// parseRateLimit convertit "N/unit" en rate.Limit. Identique à RateSpec mais
// retourne directement un rate.Limit pour le rate-limiter HTTP.
func parseRateLimit(s string) (rate.Limit, error) {
	var count int
	var unit string
	if _, err := fmt.Sscanf(s, "%d/%s", &count, &unit); err != nil {
		return 0, fmt.Errorf("invalid rate %q, expected N/minute, N/second or N/hour", s)
	}
	if count <= 0 {
		return 0, fmt.Errorf("rate count must be > 0, got %d", count)
	}
	var window time.Duration
	switch unit {
	case "second":
		window = time.Second
	case "minute":
		window = time.Minute
	case "hour":
		window = time.Hour
	default:
		return 0, fmt.Errorf("unknown rate unit %q, expected second/minute/hour", unit)
	}
	return rate.Every(window / time.Duration(count)), nil
}

func getEnvDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// loadAPIKeys parse les variables LEASH_APIKEY_<NAME>=<rawkey>, ainsi que
// les surcharges optionnelles LEASH_APIKEY_<NAME>_WORKSPACE_ID,
// LEASH_APIKEY_<NAME>_ENV (liste "K1=V1,K2=V2") et LEASH_APIKEY_<NAME>_POLICY
// (chemin vers un fichier de policy YAML propre à cette clé). La clé brute
// n'est jamais stockée : seul son empreinte sha256 persiste dans
// APIKeyConfig.keyHash. Si _POLICY est renseigné, le fichier est chargé une
// première fois ici pour échouer rapidement (fail-fast) en cas de chemin
// invalide ou de YAML malformé.
func loadAPIKeys(environ []string) ([]*APIKeyConfig, error) {
	env := make(map[string]string, len(environ))
	for _, kv := range environ {
		k, v, ok := strings.Cut(kv, "=")
		if !ok {
			continue
		}
		env[k] = v
	}

	var names []string
	for k := range env {
		if !strings.HasPrefix(k, apiKeyPrefix) {
			continue
		}
		rest := strings.TrimPrefix(k, apiKeyPrefix)
		if strings.HasSuffix(rest, "_WORKSPACE_ID") || strings.HasSuffix(rest, "_ENV") || strings.HasSuffix(rest, "_POLICY") {
			continue
		}
		names = append(names, rest)
	}

	keys := make([]*APIKeyConfig, 0, len(names))
	for _, name := range names {
		if !apiKeyNamePattern.MatchString(name) {
			return nil, fmt.Errorf("invalid API key name %q (allowed: letters, digits, '_', '-')", name)
		}

		rawKey := env[apiKeyPrefix+name]
		if rawKey == "" {
			return nil, fmt.Errorf("API key %q has an empty value", name)
		}
		if len(rawKey) < minAPIKeyLen {
			return nil, fmt.Errorf("API key %q must be at least %d characters (got %d) — use a cryptographically random value",
				name, minAPIKeyLen, len(rawKey))
		}

		cfg := &APIKeyConfig{
			Name:        name,
			keyHash:     sha256.Sum256([]byte(rawKey)),
			WorkspaceID: env[apiKeyPrefix+name+"_WORKSPACE_ID"],
		}

		if policyFile := env[apiKeyPrefix+name+"_POLICY"]; policyFile != "" {
			if err := validatePolicyFile(policyFile); err != nil {
				return nil, fmt.Errorf("API key %q: policy file %q: %w", name, policyFile, err)
			}
			cfg.PolicyFile = policyFile
		}

		if rawEnv := env[apiKeyPrefix+name+"_ENV"]; rawEnv != "" {
			pairs, err := parseEnvList(rawEnv)
			if err != nil {
				return nil, fmt.Errorf("API key %q: %w", name, err)
			}
			cfg.Env = pairs
		}

		keys = append(keys, cfg)
	}

	return keys, nil
}

// validatePolicyFile valide un fichier de policy par clé au boot
// (fail-fast) : il doit être lisible, syntaxiquement correct en tant que
// Go template (cf. policytemplate.go), et produire un YAML valide une fois
// rendu avec des valeurs factices à la place de {{.WorkspaceDir}} /
// {{.WorkspaceID}} (les vraies valeurs ne sont connues qu'à la création
// effective d'un workspace, cf. ProductionFactory).
func validatePolicyFile(path string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading: %w", err)
	}

	rendered, err := renderPolicyTemplate(raw, filepath.Base(path), policyTemplateData{
		WorkspaceDir: "/tmp/leash-validate-placeholder",
		WorkspaceID:  "validate-placeholder",
	})
	if err != nil {
		return err
	}

	tmp, err := os.CreateTemp("", "leash-policy-validate-*.yaml")
	if err != nil {
		return fmt.Errorf("creating temp file for validation: %w", err)
	}
	defer os.Remove(tmp.Name())

	if _, err := tmp.Write(rendered); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("writing temp file for validation: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing temp file for validation: %w", err)
	}

	if _, err := security.LoadPolicyConfig(tmp.Name()); err != nil {
		return fmt.Errorf("parsing rendered policy: %w", err)
	}
	return nil
}

// parseEnvList parse une liste "K1=V1,K2=V2" en map.
func parseEnvList(raw string) (map[string]string, error) {
	out := make(map[string]string)
	for part := range strings.SplitSeq(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		k, v, ok := strings.Cut(part, "=")
		if !ok {
			return nil, fmt.Errorf("invalid env entry %q, expected KEY=VALUE", part)
		}
		out[k] = v
	}
	return out, nil
}
