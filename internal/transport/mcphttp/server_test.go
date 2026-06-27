package mcphttp

import (
	"context"
	"crypto/sha256"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/bornholm/leash/pkg/leash"
)

// testFactory construit un Engine réel mais sans bubblewrap (sandbox
// "none"), pour tester l'intégration HTTP/MCP sans dépendre de
// l'environnement OS du CI.
func testFactory(_ context.Context, _ string, key *APIKeyConfig) (leash.Engine, func(), error) {
	return leash.New(context.Background(),
		leash.WithBuiltinsDisabled(),
		leash.WithAllowedBinaries("echo"),
		leash.WithStaticEnv(key.Env),
	)
}

func newTestServer(t *testing.T) (*httptest.Server, *ServerConfig) {
	t.Helper()
	root := t.TempDir()
	cfg := &ServerConfig{
		hmacSecret:    []byte("test-hmac-secret"),
		WorkspaceRoot: root,
		TTL:           time.Hour,
		DiscHeader:    "X-Workspace",
		DiscURLParam:  "workspace",
		APIKeys: []*APIKeyConfig{
			{Name: "default", keyHash: sha256.Sum256([]byte("valid-key"))},
		},
	}
	mgr := NewManager(cfg, testFactory)
	srv := NewServer(cfg, mgr)
	ts := httptest.NewServer(srv)
	t.Cleanup(func() {
		ts.Close()
		mgr.Shutdown()
	})
	return ts, cfg
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func authedClient(bearer, discHeader, discValue string) *http.Client {
	return &http.Client{
		Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
			if bearer != "" {
				r.Header.Set("Authorization", "Bearer "+bearer)
			}
			if discHeader != "" && discValue != "" {
				r.Header.Set(discHeader, discValue)
			}
			return http.DefaultTransport.RoundTrip(r)
		}),
	}
}

func TestServer_NoAuthorizationReturns401(t *testing.T) {
	ts, _ := newTestServer(t)

	resp, err := http.Post(ts.URL, "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

func TestServer_HealthzReturns200WithoutAuth(t *testing.T) {
	ts, _ := newTestServer(t)

	resp, err := http.Get(ts.URL + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
}

func TestServer_SecurityHeadersPresent(t *testing.T) {
	ts, _ := newTestServer(t)

	resp, err := http.Get(ts.URL + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	defer resp.Body.Close()

	for _, h := range []string{"X-Content-Type-Options", "X-Frame-Options", "Referrer-Policy"} {
		if resp.Header.Get(h) == "" {
			t.Errorf("missing security header %q", h)
		}
	}
}

func TestServer_RequestIDPropagated(t *testing.T) {
	ts, _ := newTestServer(t)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/healthz", nil)
	req.Header.Set("X-Request-ID", "test-id-12345")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	defer resp.Body.Close()

	if got := resp.Header.Get("X-Request-ID"); got != "test-id-12345" {
		t.Fatalf("X-Request-ID = %q, want test-id-12345", got)
	}
}

func TestServer_RequestIDGeneratedWhenAbsent(t *testing.T) {
	ts, _ := newTestServer(t)

	resp, err := http.Get(ts.URL + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	defer resp.Body.Close()

	if id := resp.Header.Get("X-Request-ID"); id == "" {
		t.Fatal("expected X-Request-ID to be generated when absent from request")
	}
}

func callExecuteShell(t *testing.T, ctx context.Context, ts *httptest.Server, httpClient *http.Client, script string) string {
	t.Helper()

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0.0.1"}, nil)
	transport := &mcp.StreamableClientTransport{Endpoint: ts.URL, HTTPClient: httpClient}

	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer session.Close()

	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "execute_shell",
		Arguments: map[string]any{"script": script},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}

	var sb strings.Builder
	for _, c := range result.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			sb.WriteString(tc.Text)
		}
	}
	return sb.String()
}

func TestServer_NoDiscriminantCreatesEphemeralWorkspace(t *testing.T) {
	ts, _ := newTestServer(t)
	ctx := context.Background()

	// Client authentifié mais sans header de discriminant.
	httpClient := authedClient("valid-key", "", "")
	out := callExecuteShell(t, ctx, ts, httpClient, "echo ephemeral")

	if !strings.Contains(out, "ephemeral") {
		t.Fatalf("expected output to contain 'ephemeral', got %q", out)
	}
}

func TestServer_EphemeralWorkspaceIsEvictedOnSessionClose(t *testing.T) {
	ts, cfg := newTestServer(t)
	ctx := context.Background()

	httpClient := authedClient("valid-key", "", "")

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0.0.1"}, nil)
	transport := &mcp.StreamableClientTransport{Endpoint: ts.URL, HTTPClient: httpClient}

	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}

	// Exécuter une commande pour s'assurer que le workspace existe.
	if _, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "execute_shell",
		Arguments: map[string]any{"script": "echo hi"},
	}); err != nil {
		t.Fatalf("CallTool: %v", err)
	}

	// Vérifier qu'un workspace éphémère a bien été créé.
	entries, err := os.ReadDir(cfg.WorkspaceRoot)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 1 || !strings.HasPrefix(entries[0].Name(), "eph-") {
		t.Fatalf("expected exactly one eph- workspace dir, got %v", entries)
	}
	wsDir := filepath.Join(cfg.WorkspaceRoot, entries[0].Name())

	// Fermer la session : le serveur doit évincer le workspace.
	session.Close()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(wsDir); os.IsNotExist(err) {
			return // eviction OK
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("ephemeral workspace dir %q still exists after session close", wsDir)
}

func TestServer_AuthenticatedRequestCreatesWorkspaceAndExecutes(t *testing.T) {
	ts, cfg := newTestServer(t)
	ctx := context.Background()

	httpClient := authedClient("valid-key", cfg.DiscHeader, "tenant-a")
	out := callExecuteShell(t, ctx, ts, httpClient, "echo hello")

	if !strings.Contains(out, "hello") {
		t.Fatalf("expected output to contain 'hello', got %q", out)
	}

	wantID, err := hashDiscriminator(cfg.hmacSecret, "tenant-a")
	if err != nil {
		t.Fatal(err)
	}
	if _, statErr := os.Stat(filepath.Join(cfg.WorkspaceRoot, wantID)); statErr != nil {
		t.Fatalf("expected workspace dir for tenant-a to exist: %v", statErr)
	}
}

func TestServer_DistinctDiscriminantsGetDistinctDirs(t *testing.T) {
	ts, cfg := newTestServer(t)
	ctx := context.Background()

	callExecuteShell(t, ctx, ts, authedClient("valid-key", cfg.DiscHeader, "tenant-a"), "echo a")
	callExecuteShell(t, ctx, ts, authedClient("valid-key", cfg.DiscHeader, "tenant-b"), "echo b")

	idA, _ := hashDiscriminator(cfg.hmacSecret, "tenant-a")
	idB, _ := hashDiscriminator(cfg.hmacSecret, "tenant-b")

	if idA == idB {
		t.Fatalf("expected distinct hashes for distinct discriminants")
	}
	if _, err := os.Stat(filepath.Join(cfg.WorkspaceRoot, idA)); err != nil {
		t.Fatalf("tenant-a dir missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(cfg.WorkspaceRoot, idB)); err != nil {
		t.Fatalf("tenant-b dir missing: %v", err)
	}
}

func TestServer_PathTraversalDiscriminantStaysUnderWorkspaceRoot(t *testing.T) {
	ts, cfg := newTestServer(t)
	ctx := context.Background()

	malicious := "../../../../etc/passwd"
	callExecuteShell(t, ctx, ts, authedClient("valid-key", cfg.DiscHeader, malicious), "echo pwned")

	id, err := hashDiscriminator(cfg.hmacSecret, malicious)
	if err != nil {
		t.Fatal(err)
	}

	entries, err := os.ReadDir(cfg.WorkspaceRoot)
	if err != nil {
		t.Fatalf("ReadDir(WorkspaceRoot): %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != id {
		t.Fatalf("expected exactly one dir named %q under WorkspaceRoot, got %v", id, entries)
	}

	resolved, err := filepath.Abs(filepath.Join(cfg.WorkspaceRoot, entries[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	rootAbs, err := filepath.Abs(cfg.WorkspaceRoot)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(resolved, rootAbs) {
		t.Fatalf("workspace dir %q escaped WorkspaceRoot %q", resolved, rootAbs)
	}
}
