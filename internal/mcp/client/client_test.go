package client

import (
	"context"
	"testing"
	"time"

	"github.com/bornholm/leash/internal/security"
)

// TestConnect_UnreachableHTTPServerFailsWithinConfiguredTimeout vérifie
// qu'un serveur MCP HTTP injoignable (port fermé) ne bloque pas Connect
// indéfiniment : sans cette borne, leash.New (et donc Manager.Acquire côté
// mcphttp) attendrait jusqu'à expiration du contexte appelant, qui peut être
// sans deadline (cf. http.Request.Context() d'une requête MCP initialize).
func TestConnect_UnreachableHTTPServerFailsWithinConfiguredTimeout(t *testing.T) {
	cfg := security.MCPServerConfig{
		Name:      "unreachable",
		Transport: "http",
		URL:       "http://10.255.255.1:81/never-listens", // adresse non routable (TEST-NET, RFC 5737-like) : les paquets sont silencieusement abandonnés, ce qui ferait pendre la connexion sans borne
		Timeout:   security.Duration{Duration: 500 * time.Millisecond},
	}

	start := time.Now()
	_, err := Connect(context.Background(), cfg)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected an error connecting to an unreachable MCP server")
	}
	if elapsed > 5*time.Second {
		t.Fatalf("Connect took %v, expected it to fail fast (bounded by cfg.Timeout)", elapsed)
	}
}

// TestConnect_DefaultTimeoutAppliedWhenUnset vérifie que l'absence de
// cfg.Timeout dans la policy applique bien defaultMCPConnectTimeout, et non
// un blocage indéfini sur le ctx appelant (qui peut être sans deadline).
func TestConnect_DefaultTimeoutAppliedWhenUnset(t *testing.T) {
	cfg := security.MCPServerConfig{
		Name:      "unreachable-default-timeout",
		Transport: "http",
		URL:       "http://10.255.255.1:81/never-listens",
	}

	start := time.Now()
	_, err := Connect(context.Background(), cfg)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected an error connecting to an unreachable MCP server")
	}
	if elapsed > defaultMCPConnectTimeout+5*time.Second {
		t.Fatalf("Connect took %v, expected it to be bounded by defaultMCPConnectTimeout (%v)", elapsed, defaultMCPConnectTimeout)
	}
}
