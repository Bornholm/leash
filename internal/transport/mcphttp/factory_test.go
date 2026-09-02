package mcphttp

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bornholm/leash/internal/security/sandbox"
)

func TestWorkspaceSandbox_InjectsWorkspaceDirAsReadWriteBind(t *testing.T) {
	cfg := sandbox.Config{} // fichier de policy sans section sandbox

	out := workspaceSandbox("", cfg, "/srv/leash/workspaces/abc123")

	if !out.Enabled {
		t.Fatal("expected sandbox to be enabled (C2 floor)")
	}
	if out.Backend != "bwrap" {
		t.Fatalf("backend = %q, want bwrap forced (C2 floor)", out.Backend)
	}
	if out.Workdir != "/work" {
		t.Fatalf("Workdir = %q, want default /work", out.Workdir)
	}
	if len(out.ReadwriteBinds) != 1 || out.ReadwriteBinds[0] != (sandbox.BindMount{Source: "/srv/leash/workspaces/abc123", Target: "/work"}) {
		t.Fatalf("ReadwriteBinds = %+v, want a single bind for the workspace dir", out.ReadwriteBinds)
	}
}

func TestWorkspaceSandbox_PreservesCustomSandboxSettings(t *testing.T) {
	cfg := sandbox.Config{
		Backend: "bwrap",
		Workdir: "/app",
		ReadonlyBinds: []string{
			"/usr",
		},
		ReadwriteBinds: []sandbox.BindMount{
			{Source: "/some/extra/dir", Target: "/extra"},
		},
		Unshare: sandbox.Unshare{
			Network: false, // une clé peut explicitement autoriser le réseau
			PID:     true,
		},
	}

	out := workspaceSandbox("", cfg, "/srv/leash/workspaces/tenant-b")

	if out.Unshare.Network {
		t.Fatal("expected Unshare.Network to stay false, as set by the per-key policy file")
	}
	if !out.Unshare.PID {
		t.Fatal("expected Unshare.PID to be preserved from the per-key policy file")
	}
	if out.Workdir != "/app" {
		t.Fatalf("Workdir = %q, want the file's custom /app preserved", out.Workdir)
	}
	if len(out.ReadonlyBinds) != 1 || out.ReadonlyBinds[0] != "/usr" {
		t.Fatalf("ReadonlyBinds = %+v, want preserved from file", out.ReadonlyBinds)
	}

	// Le bind additionnel du fichier doit être conservé, et le bind du
	// workspace doit être ajouté (pas substitué) sur le Workdir du fichier.
	want := []sandbox.BindMount{
		{Source: "/some/extra/dir", Target: "/extra"},
		{Source: "/srv/leash/workspaces/tenant-b", Target: "/app"},
	}
	if len(out.ReadwriteBinds) != len(want) {
		t.Fatalf("ReadwriteBinds = %+v, want %+v", out.ReadwriteBinds, want)
	}
	for i := range want {
		if out.ReadwriteBinds[i] != want[i] {
			t.Fatalf("ReadwriteBinds[%d] = %+v, want %+v", i, out.ReadwriteBinds[i], want[i])
		}
	}
}

func TestWorkspaceSandbox_ForcesBwrapEvenIfFileDisablesSandbox(t *testing.T) {
	cfg := sandbox.Config{Enabled: false, Backend: "none"}

	out := workspaceSandbox("", cfg, "/srv/leash/workspaces/tenant-c")

	if !out.Enabled || out.Backend != "bwrap" {
		t.Fatalf("expected bwrap to be forced regardless of the file (C2 floor), got Enabled=%v Backend=%q", out.Enabled, out.Backend)
	}
}

func TestProductionFactory_DefaultsToHardenedSandboxAndDisabledBuiltinsWithoutPolicyFile(t *testing.T) {
	key := &APIKeyConfig{Name: "default"}
	dir := t.TempDir()

	eng, cleanup, err := ProductionFactory("")(t.Context(), dir, key)
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	defer cleanup()

	// Un builtin quelconque doit être bloqué : aucun n'est enregistré ni
	// activé par défaut (C4), donc "leash-help" tombe en commande inconnue.
	result, err := eng.Exec(t.Context(), "leash-help")
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if result.ExitCode == 0 {
		t.Fatalf("expected builtins disabled by default, got exit code 0")
	}
}

func TestProductionFactory_PolicyFileReceivesRealWorkspaceDir(t *testing.T) {
	policyPath := writeTempPolicy(t, `
allowed_binaries: ["echo"]
environment:
  static:
    RESOLVED_DIR: "{{.WorkspaceDir}}"
    RESOLVED_ID: "{{.WorkspaceID}}"
`)
	dir := t.TempDir()
	key := &APIKeyConfig{Name: "tenant", PolicyFile: policyPath}

	eng, cleanup, err := ProductionFactory("")(t.Context(), dir, key)
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	defer cleanup()

	var stdout strings.Builder
	result, err := eng.ExecWithStreams(t.Context(), "echo $RESOLVED_DIR:$RESOLVED_ID", nil, &stdout, io.Discard)
	if err != nil {
		t.Fatalf("ExecWithStreams: %v", err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("exit code = %d, want 0", result.ExitCode)
	}

	want := dir + ":" + filepath.Base(dir)
	if got := strings.TrimSpace(stdout.String()); got != want {
		t.Fatalf("output = %q, want %q (the literal real workspace dir/id, not the template placeholder)", got, want)
	}
}

func TestProductionFactory_ResolvedPolicyFileIsNeverInsideWorkspaceDir(t *testing.T) {
	policyPath := writeTempPolicy(t, `
allowed_binaries: ["echo"]
mcp_servers: []
environment:
  static:
    SECRET_LOOKING_VALUE: "should-not-leak-into-the-sandbox"
`)
	dir := t.TempDir()
	key := &APIKeyConfig{Name: "tenant", PolicyFile: policyPath}

	eng, cleanup, err := ProductionFactory("")(t.Context(), dir, key)
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	defer cleanup()
	_ = eng

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir(dir): %v", err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), "policy") {
			t.Fatalf("resolved policy file leaked into the bind-mounted workspace dir: %s", e.Name())
		}
	}
}

func TestProductionFactory_CleanupRemovesResolvedPolicyFile(t *testing.T) {
	policyPath := writeTempPolicy(t, `allowed_binaries: ["echo"]`)
	dir := t.TempDir()
	key := &APIKeyConfig{Name: "tenant", PolicyFile: policyPath}

	before, _ := os.ReadDir(os.TempDir())

	_, cleanup, err := ProductionFactory("")(t.Context(), dir, key)
	if err != nil {
		t.Fatalf("factory: %v", err)
	}

	var resolved string
	after, _ := os.ReadDir(os.TempDir())
	for _, e := range after {
		if strings.HasPrefix(e.Name(), "leash-policy-") && !containsName(before, e.Name()) {
			resolved = filepath.Join(os.TempDir(), e.Name())
			break
		}
	}
	if resolved == "" {
		t.Fatal("expected a resolved policy temp file to be created in os.TempDir()")
	}
	if _, err := os.Stat(resolved); err != nil {
		t.Fatalf("expected resolved policy file to exist before cleanup: %v", err)
	}

	cleanup()

	if _, err := os.Stat(resolved); !os.IsNotExist(err) {
		t.Fatalf("expected resolved policy file to be removed after cleanup, stat err = %v", err)
	}
}

func containsName(entries []os.DirEntry, name string) bool {
	for _, e := range entries {
		if e.Name() == name {
			return true
		}
	}
	return false
}

func TestProductionFactory_InvalidTemplateFailsAtFactoryCall(t *testing.T) {
	policyPath := writeTempPolicy(t, `
allowed_binaries: ["echo"]
environment:
  static:
    BAD: "{{.NotAField}}"
`)
	dir := t.TempDir()
	key := &APIKeyConfig{Name: "tenant", PolicyFile: policyPath}

	if _, _, err := ProductionFactory("")(t.Context(), dir, key); err == nil {
		t.Fatal("expected an error for a template referencing an unknown field")
	}
}

func TestForceBackend_ChrootIgnoresPolicyBackendAndBinds(t *testing.T) {
	// Une policy par clé qui réclame bwrap (ou n'importe quoi d'autre) ne
	// peut pas contredire le backend choisi par l'opérateur.
	cfg := sandbox.Config{
		Enabled: true,
		Backend: "bwrap",
		Workdir: "/work",
		ReadwriteBinds: []sandbox.BindMount{
			{Source: "/etc/secrets", Target: "/secrets"},
		},
	}

	out := forceBackend(cfg, SandboxBackendChroot, "/srv/leash/workspaces/tenant-d")

	if out.Backend != SandboxBackendChroot {
		t.Fatalf("Backend = %q, want %q", out.Backend, SandboxBackendChroot)
	}
	if out.Rootfs != "/" {
		t.Fatalf("Rootfs = %q, want /", out.Rootfs)
	}
	if out.Workdir != "/srv/leash/workspaces/tenant-d" {
		t.Fatalf("Workdir = %q, want le répertoire réel du workspace", out.Workdir)
	}
	if len(out.ReadwriteBinds) != 0 || len(out.ReadonlyBinds) != 0 {
		t.Fatalf("binds conservés alors que chroot ne sait pas les honorer: %+v", out)
	}
}

func TestForceBackend_PolicyCannotDisableSandbox(t *testing.T) {
	cfg := sandbox.Config{Enabled: false, Backend: "none"}

	out := forceBackend(cfg, SandboxBackendBwrap, "/srv/leash/workspaces/tenant-e")

	if !out.Enabled {
		t.Fatal("le sandbox a pu être désactivé par la policy")
	}
	if out.Backend != SandboxBackendBwrap {
		t.Fatalf("Backend = %q, want %q", out.Backend, SandboxBackendBwrap)
	}
}

func TestHardenedSandbox_ChrootBackend(t *testing.T) {
	out := hardenedSandbox(SandboxBackendChroot, "/srv/leash/workspaces/tenant-f")
	if out.Backend != SandboxBackendChroot {
		t.Fatalf("Backend = %q, want chroot", out.Backend)
	}
	if out.Workdir != "/srv/leash/workspaces/tenant-f" {
		t.Fatalf("Workdir = %q", out.Workdir)
	}
}
