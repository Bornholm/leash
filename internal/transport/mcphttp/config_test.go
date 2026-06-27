package mcphttp

import (
	"crypto/sha256"
	"os"
	"testing"
	"time"
)

// Valeurs de test respectant les longueurs minimales d'entropie.
const (
	testHMACSecret    = "test-hmac-secret-for-unit-tests-only-00"  // 39 chars ≥ 32
	testDefaultAPIKey = "raw-api-key-for-unit-tests-ok-here-yes"   // 38 chars ≥ 20
	testTenantAAPIKey = "tenant-a-api-key-for-unit-tests-ok-yes"   // 38 chars ≥ 20
)

func TestLoadConfig_FailsFastWithoutSecret(t *testing.T) {
	t.Setenv(envHMACSecret, "")
	t.Setenv(apiKeyPrefix+"DEFAULT", testDefaultAPIKey)

	if _, err := LoadConfig(); err == nil {
		t.Fatal("expected error when LEASH_HMAC_SECRET is absent")
	}
}

func TestLoadConfig_FailsWithoutAnyAPIKey(t *testing.T) {
	t.Setenv(envHMACSecret, testHMACSecret)
	clearAPIKeyEnv(t)

	if _, err := LoadConfig(); err == nil {
		t.Fatal("expected error when no API key is configured")
	}
}

func TestLoadConfig_DefaultsAndOneKey(t *testing.T) {
	t.Setenv(envHMACSecret, testHMACSecret)
	clearAPIKeyEnv(t)
	t.Setenv(apiKeyPrefix+"DEFAULT", testDefaultAPIKey)

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}

	if cfg.WorkspaceRoot != defaultWorkspaceRoot {
		t.Errorf("WorkspaceRoot = %q, want default %q", cfg.WorkspaceRoot, defaultWorkspaceRoot)
	}
	if cfg.TTL != defaultTTL {
		t.Errorf("TTL = %v, want default %v", cfg.TTL, defaultTTL)
	}
	if cfg.DiscHeader != defaultDiscHeader {
		t.Errorf("DiscHeader = %q, want default %q", cfg.DiscHeader, defaultDiscHeader)
	}
	if cfg.DiscURLParam != defaultDiscURLParam {
		t.Errorf("DiscURLParam = %q, want default %q", cfg.DiscURLParam, defaultDiscURLParam)
	}
	if len(cfg.APIKeys) != 1 {
		t.Fatalf("expected 1 API key, got %d", len(cfg.APIKeys))
	}
	if cfg.APIKeys[0].Name != "DEFAULT" {
		t.Errorf("APIKeys[0].Name = %q, want DEFAULT", cfg.APIKeys[0].Name)
	}
}

func TestLoadConfig_RawKeyNeverStored(t *testing.T) {
	t.Setenv(envHMACSecret, testHMACSecret)
	clearAPIKeyEnv(t)
	t.Setenv(apiKeyPrefix+"DEFAULT", "super-secret-raw-value")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}

	want := sha256.Sum256([]byte("super-secret-raw-value"))
	if cfg.APIKeys[0].keyHash != want {
		t.Fatalf("keyHash does not match sha256 of the raw value")
	}

	wrong := sha256.Sum256([]byte("wrong-value"))
	if cfg.APIKeys[0].keyHash == wrong {
		t.Fatalf("keyHash unexpectedly matches an unrelated value")
	}
}

func TestLoadConfig_InvalidTTL(t *testing.T) {
	t.Setenv(envHMACSecret, testHMACSecret)
	clearAPIKeyEnv(t)
	t.Setenv(apiKeyPrefix+"DEFAULT", testDefaultAPIKey)
	t.Setenv(envTTL, "not-a-duration")

	if _, err := LoadConfig(); err == nil {
		t.Fatal("expected error for invalid TTL")
	}
}

func TestLoadConfig_TTLOverride(t *testing.T) {
	t.Setenv(envHMACSecret, testHMACSecret)
	clearAPIKeyEnv(t)
	t.Setenv(apiKeyPrefix+"DEFAULT", testDefaultAPIKey)
	t.Setenv(envTTL, "5m")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.TTL != 5*time.Minute {
		t.Errorf("TTL = %v, want 5m", cfg.TTL)
	}
}

func TestLoadConfig_WorkspaceIDAndEnvOverrides(t *testing.T) {
	t.Setenv(envHMACSecret, testHMACSecret)
	clearAPIKeyEnv(t)
	t.Setenv(apiKeyPrefix+"TENANTA", testTenantAAPIKey)
	t.Setenv(apiKeyPrefix+"TENANTA_WORKSPACE_ID", "fixed-ws")
	t.Setenv(apiKeyPrefix+"TENANTA_ENV", "FOO=bar, BAZ=qux")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if len(cfg.APIKeys) != 1 {
		t.Fatalf("expected 1 API key, got %d", len(cfg.APIKeys))
	}
	key := cfg.APIKeys[0]
	if key.WorkspaceID != "fixed-ws" {
		t.Errorf("WorkspaceID = %q, want fixed-ws", key.WorkspaceID)
	}
	if key.Env["FOO"] != "bar" || key.Env["BAZ"] != "qux" {
		t.Errorf("Env = %+v, want FOO=bar, BAZ=qux", key.Env)
	}
}

func TestLoadConfig_PerKeyPolicyFile(t *testing.T) {
	t.Setenv(envHMACSecret, testHMACSecret)
	clearAPIKeyEnv(t)
	t.Setenv(apiKeyPrefix+"TENANTA", testTenantAAPIKey)

	policyPath := writeTempPolicy(t, `
allowed_binaries: ["curl"]
builtins:
  enabled: ["leash-help"]
mcp_servers:
  - name: filesystem
    transport: stdio
    command: ["npx", "-y", "@modelcontextprotocol/server-filesystem", "{{.WorkspaceDir}}"]
`)
	t.Setenv(apiKeyPrefix+"TENANTA_POLICY", policyPath)

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if len(cfg.APIKeys) != 1 {
		t.Fatalf("expected 1 API key, got %d", len(cfg.APIKeys))
	}
	if cfg.APIKeys[0].PolicyFile != policyPath {
		t.Errorf("PolicyFile = %q, want %q", cfg.APIKeys[0].PolicyFile, policyPath)
	}
}

func TestLoadConfig_PerKeyPolicyFileFailsFastOnInvalidPath(t *testing.T) {
	t.Setenv(envHMACSecret, testHMACSecret)
	clearAPIKeyEnv(t)
	t.Setenv(apiKeyPrefix+"TENANTA", testTenantAAPIKey)
	t.Setenv(apiKeyPrefix+"TENANTA_POLICY", "/does/not/exist.yaml")

	if _, err := LoadConfig(); err == nil {
		t.Fatal("expected error for a non-existent per-key policy file")
	}
}

func TestLoadConfig_PerKeyPolicyFileFailsFastOnMalformedYAML(t *testing.T) {
	t.Setenv(envHMACSecret, testHMACSecret)
	clearAPIKeyEnv(t)
	t.Setenv(apiKeyPrefix+"TENANTA", testTenantAAPIKey)

	policyPath := writeTempPolicy(t, "not: [valid: yaml")
	t.Setenv(apiKeyPrefix+"TENANTA_POLICY", policyPath)

	if _, err := LoadConfig(); err == nil {
		t.Fatal("expected error for malformed per-key policy YAML")
	}
}

func TestLoadConfig_PerKeyPolicyFileFailsFastOnInvalidTemplate(t *testing.T) {
	t.Setenv(envHMACSecret, testHMACSecret)
	clearAPIKeyEnv(t)
	t.Setenv(apiKeyPrefix+"TENANTA", testTenantAAPIKey)

	policyPath := writeTempPolicy(t, `
environment:
  static:
    BAD: "{{.NotAField}}"
`)
	t.Setenv(apiKeyPrefix+"TENANTA_POLICY", policyPath)

	if _, err := LoadConfig(); err == nil {
		t.Fatal("expected error for a template referencing an unknown field")
	}
}

func writeTempPolicy(t *testing.T, content string) string {
	t.Helper()
	path := t.TempDir() + "/policy.yaml"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writing temp policy file: %v", err)
	}
	return path
}

func TestLoadConfig_InvalidEnvOverride(t *testing.T) {
	t.Setenv(envHMACSecret, testHMACSecret)
	clearAPIKeyEnv(t)
	t.Setenv(apiKeyPrefix+"DEFAULT", testDefaultAPIKey)
	t.Setenv(apiKeyPrefix+"DEFAULT_ENV", "not-a-kv-pair")

	if _, err := LoadConfig(); err == nil {
		t.Fatal("expected error for malformed _ENV override")
	}
}

// clearAPIKeyEnv neutralise les variables LEASH_APIKEY_* qui pourraient
// déjà être présentes dans l'environnement du processus de test, pour
// isoler chaque cas. t.Setenv("X", "") laisse la variable présente avec une
// valeur vide (os.Environ() la voit toujours) ; il faut donc un vrai
// os.Unsetenv, restauré après le test.
func clearAPIKeyEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		apiKeyPrefix + "DEFAULT",
		apiKeyPrefix + "DEFAULT_ENV",
		apiKeyPrefix + "DEFAULT_WORKSPACE_ID",
		apiKeyPrefix + "DEFAULT_POLICY",
		apiKeyPrefix + "TENANTA",
		apiKeyPrefix + "TENANTA_WORKSPACE_ID",
		apiKeyPrefix + "TENANTA_ENV",
		apiKeyPrefix + "TENANTA_POLICY",
	} {
		prev, had := os.LookupEnv(k)
		_ = os.Unsetenv(k)
		t.Cleanup(func() {
			if had {
				_ = os.Setenv(k, prev)
			}
		})
	}
}
